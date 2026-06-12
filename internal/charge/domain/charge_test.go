package domain

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
)

func validParams() NewImmediateParams {
	return NewImmediateParams{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	}
}

func TestNewTxidFormat(t *testing.T) {
	re := regexp.MustCompile(`^[a-zA-Z0-9]{26,35}$`)
	for i := 0; i < 100; i++ {
		require.Regexp(t, re, NewTxid())
	}
}

func TestNewImmediateCreatesCreated(t *testing.T) {
	c, err := NewImmediate(validParams())
	require.NoError(t, err)
	require.Equal(t, KindCob, c.Kind)
	require.Equal(t, StatusCreated, c.Status)
	require.NotEmpty(t, c.ID)
	require.NotEmpty(t, c.Txid)
	require.Equal(t, 0, c.Version)
	require.Len(t, c.Events, 1)
	require.Equal(t, "created", c.Events[0].EventType)
}

func TestNewImmediateValidates(t *testing.T) {
	p := validParams()
	p.Amount = 0
	_, err := NewImmediate(p)
	require.Error(t, err)

	p = validParams()
	p.PixKey = ""
	_, err = NewImmediate(p)
	require.Error(t, err)
}
