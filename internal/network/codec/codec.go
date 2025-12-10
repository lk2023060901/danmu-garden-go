package codec

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/lk2023060901/danmu-garden-go/internal/network/compressor"
	"github.com/lk2023060901/danmu-garden-go/internal/network/crypto"
	"github.com/lk2023060901/danmu-garden-go/internal/network/framer"
	"github.com/lk2023060901/danmu-garden-go/internal/network/serializer"
	protos "github.com/lk2023060901/danmu-garden-framework-protos/framework"
)

// Codec 抽象了“从业务对象到网络帧，以及从网络帧回到业务对象”的完整编解码流程。
//
// Pipeline（写出 Encode）：
//   msg --> serializer --> [compress?] --> [encrypt?] --> Envelope{Header+Payload} --> framer.WriteFrame
//
// Pipeline（读入 Decode）：
//   framer.ReadFrame --> Envelope{Header+Payload} --> [decrypt?] --> [decompress?] --> serializer --> msg
type Codec interface {
	// Encode 将业务对象编码并写入到底层流。
	//
	//   - header：由调用方构造的报文头（可为 nil，nil 时内部会创建一个空的 MessageHeader）。
	//   - msg   ：待编码的业务对象，由 serializer 负责实际序列化。
	Encode(w io.Writer, header *protos.MessageHeader, msg any) error

	// Decode 从底层流中读取一帧报文，并解码到 msg 中。
	//
	//   - msg 为接收解码结果的目标对象（通常为指针）；若为 nil，则仅解析并返回 Header。
	Decode(r io.Reader, msg any) (*protos.MessageHeader, error)

	// DecodeRaw 从底层流中读取一帧报文，并返回消息头和已完成解密/解压的业务字节。
	//
	// 说明：
	//   - 不负责反序列化为具体对象，仅返回“明文字节”供上层自行处理；
	//   - 对应 Encode 的逆过程：framer.ReadFrame -> [decrypt?] -> [decompress?]。
	DecodeRaw(r io.Reader) (*protos.MessageHeader, []byte, error)
}

// Options 用于构造 Codec 的依赖注入参数。
type Options struct {
	Framer     framer.Framer
	Serializer serializer.Serializer
	Compressor compressor.Compressor // 允许为 nil（内部会用 NopCompressor）
	Encryptor  crypto.Encryptor      // 允许为 nil（内部会用 NopEncryptor）

	EnableCompression bool // 是否启用压缩（影响压缩行为与 Header.Flags）
	EnableEncryption  bool // 是否启用加密（影响加密行为与 Header.Flags）
}

type codec struct {
	framer     framer.Framer
	serializer serializer.Serializer
	compressor compressor.Compressor
	encryptor  crypto.Encryptor

	compress bool
	encrypt  bool
}

var _ Codec = (*codec)(nil)

const (
	flagCompressed = uint64(protos.EnvelopeFlag_ENVELOPE_FLAG_COMPRESSED)
	flagEncrypted  = uint64(protos.EnvelopeFlag_ENVELOPE_FLAG_ENCRYPTED)
)

// New 创建一个基于给定依赖的 Codec。
func New(opts Options) (Codec, error) {
	if opts.Framer == nil {
		return nil, fmt.Errorf("codec: framer is nil")
	}
	if opts.Serializer == nil {
		return nil, fmt.Errorf("codec: serializer is nil")
	}

	c := &codec{
		framer:     opts.Framer,
		serializer: opts.Serializer,
		compress:   opts.EnableCompression,
		encrypt:    opts.EnableEncryption,
	}

	if opts.Compressor != nil {
		c.compressor = opts.Compressor
	} else {
		c.compressor = compressor.NopCompressor{}
	}
	if opts.Encryptor != nil {
		c.encryptor = opts.Encryptor
	} else {
		c.encryptor = crypto.NopEncryptor{}
	}

	return c, nil
}

// Encode 实现 Codec.Encode。
func (c *codec) Encode(w io.Writer, header *protos.MessageHeader, msg any) error {
	if w == nil {
		return fmt.Errorf("codec: writer is nil")
	}
	if msg == nil {
		return fmt.Errorf("codec: msg is nil")
	}
	if header == nil {
		return fmt.Errorf("codec: header is nil")
	}

	// 第一步：业务对象序列化。
	body, err := c.serializer.Marshal(msg)
	if err != nil {
		return fmt.Errorf("codec: marshal failed: %w", err)
	}

	// 在设置新 flags 之前，先清理压缩/加密相关位，避免复用 header 时遗留旧状态。
	header.Flags &^= (flagCompressed | flagEncrypted)

	// 第二步：可选压缩。
	if c.compress && len(body) > 0 {
		compressed, err := c.compressor.Compress(body)
		if err != nil {
			return fmt.Errorf("codec: compress failed: %w", err)
		}
		body = compressed
		header.Flags |= flagCompressed
	}

	// 第三步：可选加密。
	if c.encrypt && len(body) > 0 {
		aad := buildAAD(header)
		packet, err := c.encryptor.Encrypt(body, aad)
		if err != nil {
			return fmt.Errorf("codec: encrypt failed: %w", err)
		}
		body = packet
		header.Flags |= flagEncrypted
	}

	// 记录最终 payload 长度（与 framer 的 Header.Size 语义保持一致：等于 Envelope.Payload 的长度）。
	header.Size = uint32(len(body))

	env := &protos.Envelope{
		Header:  header,
		Payload: body,
	}

	if err := c.framer.WriteFrame(w, env); err != nil {
		return fmt.Errorf("codec: write frame failed: %w", err)
	}
	return nil
}

// decodeFrame 完成从底层流到“消息头 + 业务明文字节”的解码流程。
//
// Pipeline：
//   framer.ReadFrame --> Envelope{Header+Payload} --> [decrypt?] --> [decompress?]
func (c *codec) decodeFrame(r io.Reader) (*protos.MessageHeader, []byte, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("codec: reader is nil")
	}

	env, err := c.framer.ReadFrame(r)
	if err != nil {
		return nil, nil, fmt.Errorf("codec: read frame failed: %w", err)
	}

	header := env.Header
	if header == nil {
		header = &protos.MessageHeader{}
	}

	data := env.Payload

	// 第一阶段：加密 -> 解密。
	if header.Flags&flagEncrypted != 0 {
		if !c.encrypt {
			return nil, nil, fmt.Errorf("codec: encrypted payload but encryption disabled")
		}
		if len(data) == 0 {
			return nil, nil, fmt.Errorf("codec: encrypted payload is empty")
		}

		aad := buildAAD(header)
		plain, err := c.encryptor.Decrypt(data, aad)
		if err != nil {
			return nil, nil, fmt.Errorf("codec: decrypt failed: %w", err)
		}
		data = plain
	}

	// 第二阶段：压缩 -> 解压。
	if header.Flags&flagCompressed != 0 {
		if !c.compress {
			return nil, nil, fmt.Errorf("codec: compressed payload but compression disabled")
		}
		if len(data) == 0 {
			return nil, nil, fmt.Errorf("codec: compressed payload is empty")
		}

		plain, err := c.compressor.Decompress(data)
		if err != nil {
			return nil, nil, fmt.Errorf("codec: decompress failed: %w", err)
		}
		data = plain
	}

	return header, data, nil
}

// DecodeRaw 实现 Codec.DecodeRaw。
func (c *codec) DecodeRaw(r io.Reader) (*protos.MessageHeader, []byte, error) {
	return c.decodeFrame(r)
}

// Decode 实现 Codec.Decode。
func (c *codec) Decode(r io.Reader, msg any) (*protos.MessageHeader, error) {
	header, data, err := c.decodeFrame(r)
	if err != nil {
		return nil, err
	}

	// 第三阶段：反序列化到业务对象。
	if msg != nil && len(data) > 0 {
		if err := c.serializer.Unmarshal(data, msg); err != nil {
			return nil, fmt.Errorf("codec: unmarshal failed: %w", err)
		}
	}

	return header, nil
}

// buildAAD 将 MessageHeader 中与完整性相关的字段编码为 AAD。
//
// 约定：AAD 字段顺序为：
//   op(uint32) | seq(uint64) | flags(uint64) | timestamp(int64)
//
// 注意：此处不包含 size 字段，避免与“payload 最终长度”的定义产生循环依赖。
func buildAAD(h *protos.MessageHeader) []byte {
	var buf [28]byte

	binary.BigEndian.PutUint32(buf[0:4], h.Op)
	binary.BigEndian.PutUint64(buf[4:12], h.Seq)
	binary.BigEndian.PutUint64(buf[12:20], h.Flags)
	binary.BigEndian.PutUint64(buf[20:28], uint64(h.Timestamp))

	return buf[:]
}
