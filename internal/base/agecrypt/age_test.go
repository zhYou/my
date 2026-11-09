package agecrypt

import (
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
)

func TestRoundTripNativeIdentities(t *testing.T) {
	x25519, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	hybrid, err := age.GenerateHybridIdentity()
	require.NoError(t, err)

	for name, encoded := range map[string]string{
		"x25519": x25519.String() + "\n",
		"hybrid": hybrid.String() + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			key, err := ParseIdentity([]byte(encoded))
			require.NoError(t, err)
			require.NotEmpty(t, key.Recipient())
			ciphertext, err := key.Encrypt([]byte("secret-marker"))
			require.NoError(t, err)
			require.NotContains(t, string(ciphertext), "secret-marker")
			plaintext, err := key.Decrypt(ciphertext, key.Recipient())
			require.NoError(t, err)
			require.Equal(t, "secret-marker", string(plaintext))
		})
	}
}

func TestDecryptRejectsWrongIdentityAndCorruptCiphertext(t *testing.T) {
	first, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	second, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	firstKey, err := ParseIdentity([]byte(first.String()))
	require.NoError(t, err)
	secondKey, err := ParseIdentity([]byte(second.String()))
	require.NoError(t, err)
	ciphertext, err := firstKey.Encrypt([]byte("private-fixture-value"))
	require.NoError(t, err)

	_, err = secondKey.Decrypt(ciphertext, firstKey.Recipient())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-fixture-value")

	ciphertext[len(ciphertext)-1] ^= 0xff
	_, err = firstKey.Decrypt(ciphertext, firstKey.Recipient())
	require.Error(t, err)
}

func TestParseIdentityRequiresExactlyOneNativeIdentity(t *testing.T) {
	first, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	second, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	_, err = ParseIdentity([]byte(first.String() + "\n" + second.String() + "\n"))
	require.Error(t, err)
	_, err = ParseIdentity([]byte("AGE-SECRET-KEY-1-INVALID\n"))
	require.Error(t, err)
}
