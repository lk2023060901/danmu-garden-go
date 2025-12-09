package crypto

// Encryptor 抽象了单一“加密方案”的能力：
//   - Encrypt：加密 +（可选）签名/防篡改，生成完整报文
//   - Decrypt：验签 + 解密，还原明文
//
// aad（Associated Data）为关联数据，不加密但需要完整性保护（例如会话 ID、消息序号等）。
// 不同算法（AES‑GCM、ChaCha20‑Poly1305 等）都可以实现该接口，供框架使用者按需选择。
type Encryptor interface {
	Encrypt(plaintext, aad []byte) (packet []byte, err error)
	Decrypt(packet, aad []byte) (plaintext []byte, err error)
}

// NopEncryptor 是一个空实现：不做加密也不做验签，直接透传数据。
//
// 适用于：
//   - 本地开发/调试阶段，不希望引入加解密开销
//   - 按配置开关动态启用/关闭加密，而不影响调用方逻辑
type NopEncryptor struct{}

func (NopEncryptor) Encrypt(plaintext, _ []byte) ([]byte, error) {
	return plaintext, nil
}

func (NopEncryptor) Decrypt(packet, _ []byte) ([]byte, error) {
	return packet, nil
}

// 编译期断言：确保 NopEncryptor 实现了 Encryptor 接口。
var _ Encryptor = NopEncryptor{}
