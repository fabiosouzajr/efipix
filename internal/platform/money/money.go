package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Centavos is an integer amount in BRL cents.
type Centavos int64

// String renders as a decimal "reais.centavos" string for the EFí API.
func (c Centavos) String() string {
	return fmt.Sprintf("%d.%02d", c/100, abs(int64(c))%100)
}

// ParseString parses "10.50" into Centavos(1050).
func ParseString(s string) (Centavos, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ".", 2)
	reais, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: invalid amount %q: %w", s, err)
	}
	var cents int64
	if len(parts) == 2 {
		frac := (parts[1] + "00")[:2]
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("money: invalid fraction %q: %w", s, err)
		}
	}
	return Centavos(reais*100 + cents), nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
