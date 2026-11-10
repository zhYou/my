package agecrypt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"filippo.io/age"
)

const (
	maxIdentitySize   = 64 << 10
	maxPlaintextSize  = 1 << 20
	maxCiphertextSize = 2 << 20
	wrapErrorFormat   = "%w: %w"
)

// Key 是一个原生 age 身份及其对应 recipient。
type Key struct {
	identity  age.Identity
	recipient age.Recipient
	text      string
}

// ParseIdentity 解析只包含一个 X25519 或 Hybrid identity 的文本。
func ParseIdentity(data []byte) (*Key, error) {
	if len(data) == 0 || len(data) > maxIdentitySize {
		return nil, ErrInvalidIdentity
	}
	identities, err := age.ParseIdentities(bytes.NewReader(data))
	if err != nil || len(identities) != 1 {
		return nil, ErrInvalidIdentity
	}
	switch identity := identities[0].(type) {
	case *age.X25519Identity:
		recipient := identity.Recipient()
		return &Key{identity: identity, recipient: recipient, text: recipient.String()}, nil
	case *age.HybridIdentity:
		recipient := identity.Recipient()
		return &Key{identity: identity, recipient: recipient, text: recipient.String()}, nil
	default:
		return nil, ErrInvalidIdentity
	}
}

// LoadIdentity 从一个普通私有文件读取身份，不在仓内生成或复制凭据。
func LoadIdentity(path string) (*Key, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxIdentitySize {
		return nil, ErrInvalidIdentity
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, ErrInvalidIdentity
	}
	data, err := os.ReadFile(path) // #nosec G304 -- 调用方提供已显式校验的普通身份文件路径。
	if err != nil {
		return nil, err
	}
	return ParseIdentity(data)
}

// Recipient 返回目标 age 公开 recipient。
func (k *Key) Recipient() string {
	if k == nil {
		return ""
	}
	return k.text
}

// Encrypt 将明文加密给当前身份对应的 recipient。
func (k *Key) Encrypt(plaintext []byte) ([]byte, error) {
	if k == nil || k.recipient == nil || len(plaintext) == 0 || len(plaintext) > maxPlaintextSize {
		return nil, ErrCiphertext
	}
	var output bytes.Buffer
	writer, err := age.Encrypt(&output, k.recipient)
	if err != nil {
		return nil, fmt.Errorf(wrapErrorFormat, ErrCiphertext, err)
	}
	if _, err := writer.Write(plaintext); err != nil {
		return nil, fmt.Errorf(wrapErrorFormat, ErrCiphertext, err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf(wrapErrorFormat, ErrCiphertext, err)
	}
	if output.Len() == 0 || output.Len() > maxCiphertextSize {
		return nil, ErrCiphertext
	}
	return output.Bytes(), nil
}

// Decrypt 验证 recipient 并使用当前身份解密。
func (k *Key) Decrypt(ciphertext []byte, expectedRecipient string) ([]byte, error) {
	if k == nil || k.identity == nil || expectedRecipient != k.text ||
		len(ciphertext) == 0 || len(ciphertext) > maxCiphertextSize {
		return nil, ErrDecrypt
	}
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), k.identity)
	if err != nil {
		return nil, ErrDecrypt
	}
	plaintext, err := io.ReadAll(io.LimitReader(reader, maxPlaintextSize+1))
	if err != nil || len(plaintext) == 0 || len(plaintext) > maxPlaintextSize {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			return nil, ErrDecrypt
		}
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
