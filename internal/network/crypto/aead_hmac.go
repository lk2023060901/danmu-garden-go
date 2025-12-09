package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

var (
	// ErrPacketTooShort 表示加密报文长度不足，
	// 无法包含完整的 nonce、密文和 MAC。
	ErrPacketTooShort = errors.New("crypto: packet too short")

	// ErrInvalidMAC 表示 HMAC 签名校验失败。
	ErrInvalidMAC = errors.New("crypto: invalid mac")
)

const aes256KeySizeBytes = 32

// AEADHMACCodec implements "方案 A":
//   - 对称加密：AES‑256‑GCM（AEAD，提供机密性 + 完整性）
//   - 消息签名：HMAC‑SHA256（对密文和关联数据再做一层签名）
//
// 典型使用场景：
//   - plaintext：业务层序列化后的消息体
//   - aad（Associated Data）：不会加密但需要保护不被篡改的头部信息
//    （例如会话 ID、消息序号、时间戳等）
//
// 报文格式：nonce || ciphertext || mac
//   - nonce     ：随机数，长度等于 AEAD.NonceSize()
//   - ciphertext：AES‑GCM 加密后的密文（包含 GCM tag）
//   - mac       ：HMAC‑SHA256(nonce || ciphertext || aad)
type AEADHMACCodec struct {
	aead    cipher.AEAD
	hmacKey []byte
}

// 确保 AEADHMACCodec 满足 Encryptor 接口。
var _ Encryptor = (*AEADHMACCodec)(nil)

// NewAESGCMHMACCodec 使用 AES‑256‑GCM + HMAC‑SHA256 创建编码器。
//
// encKey 长度必须为 32 字节（AES‑256），macKey 为任意长度的 HMAC 密钥。
func NewAESGCMHMACCodec(encKey, macKey []byte) (*AEADHMACCodec, error) {
	if len(encKey) != aes256KeySizeBytes {
		return nil, errors.New("crypto: encKey must be 32 bytes for AES‑256‑GCM")
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(macKey) == 0 {
		return nil, errors.New("crypto: macKey must not be empty")
	}
	return &AEADHMACCodec{
		aead:    aead,
		hmacKey: append([]byte(nil), macKey...),
	}, nil
}

// EncryptAndSign 对明文进行加密并计算签名。
//
//   - plaintext：待加密的业务数据
//   - aad      ：关联数据，不会被加密，但会被 AEAD 和 HMAC 共同保护
func (c *AEADHMACCodec) EncryptAndSign(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := c.aead.Seal(nil, nonce, plaintext, aad)

	m := hmac.New(sha256.New, c.hmacKey)
	_, _ = m.Write(nonce)
	_, _ = m.Write(ciphertext)
	_, _ = m.Write(aad)
	mac := m.Sum(nil)

	packet := make([]byte, 0, len(nonce)+len(ciphertext)+len(mac))
	packet = append(packet, nonce...)
	packet = append(packet, ciphertext...)
	packet = append(packet, mac...)
	return packet, nil
}

// VerifyAndDecrypt 验证签名并解密报文。
//
//   - packet：EncryptAndSign 生成的报文
//   - aad  ：加密时使用的关联数据，必须保持一致
func (c *AEADHMACCodec) VerifyAndDecrypt(packet, aad []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(packet) < nonceSize+sha256.Size {
		return nil, ErrPacketTooShort
	}

	nonce := packet[:nonceSize]
	macOffset := len(packet) - sha256.Size
	ciphertext := packet[nonceSize:macOffset]
	macBytes := packet[macOffset:]

	m := hmac.New(sha256.New, c.hmacKey)
	_, _ = m.Write(nonce)
	_, _ = m.Write(ciphertext)
	_, _ = m.Write(aad)
	expected := m.Sum(nil)

	if !hmac.Equal(expected, macBytes) {
		return nil, ErrInvalidMAC
	}

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// Encrypt 实现通用 Codec 接口，语义等价于 EncryptAndSign。
func (c *AEADHMACCodec) Encrypt(plaintext, aad []byte) ([]byte, error) {
	return c.EncryptAndSign(plaintext, aad)
}

// Decrypt 实现通用 Codec 接口，语义等价于 VerifyAndDecrypt。
func (c *AEADHMACCodec) Decrypt(packet, aad []byte) ([]byte, error) {
	return c.VerifyAndDecrypt(packet, aad)
}
