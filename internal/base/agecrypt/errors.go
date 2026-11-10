package agecrypt

import (
	"errors"
)

var (
	// ErrInvalidIdentity 表示身份文件不是单个原生 age 身份。
	ErrInvalidIdentity = errors.New("invalid age identity")
	// ErrCiphertext 表示密文无效或超过大小限制。
	ErrCiphertext = errors.New("invalid age ciphertext")
	// ErrDecrypt 表示当前身份不能解密目标密文。
	ErrDecrypt = errors.New("age decrypt failed")
)
