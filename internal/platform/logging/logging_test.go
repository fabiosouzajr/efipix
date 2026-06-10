package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaskDoc(t *testing.T) {
	require.Equal(t, "***.***.789-**", MaskDoc("12345678900"))        // cpf 11 digits
	require.Equal(t, "**.***.***/0001-**", MaskDoc("12345678000145")) // cnpj 14 digits
	require.Equal(t, "", MaskDoc(""))
	require.Equal(t, "****", MaskDoc("ab")) // too short -> fully masked
}

func TestNewReturnsLogger(t *testing.T) {
	require.NotNil(t, New("info"))
}
