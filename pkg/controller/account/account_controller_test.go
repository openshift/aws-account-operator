package account

import (
	"fmt"
	"testing"
)

type substringTestPair struct {
	roleID string
	role   string
}

var matchPairs = []substringTestPair{
	{
		"AROA3SYAY5EP3KG4G2FIR",
		"AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
	},
	{
		"AROA3SYABCEDRKG4G2FIR",
		"AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
	},
	{
		"AROABIGORGOHOME4G2FIR",
		"AROABIGORGOHOME4G2FIR:awsAccountOperator",
	},
}

var noMatchPairs = []substringTestPair{
	{
		"IHEHRHSHY5EP3KG4G2FIR",
		"AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
	},
	{
		"AROA3SYAEHAIRHHALKBCDERKG422FIR",
		"AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
	},
	{
		"A test string",
		"AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
	},
}

func TestMatchSubstring(t *testing.T) {
	for _, pair := range matchPairs {
		result, err := matchSubstring(pair.roleID, pair.role)
		if result != true {
			t.Error(
				"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
				"expected", true,
				"got", result,
			)
		}
		if err != nil {
			t.Error(
				"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
				"expected", nil,
				"got", err,
			)
		}
	}
}

func TestNoMatchSubstring(t *testing.T) {
	for _, pair := range noMatchPairs {
		result, err := matchSubstring(pair.roleID, pair.role)
		if result != false {
			t.Error(
				"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
				"expected", false,
				"got", result,
			)
		}
		if err != nil {
			t.Error(
				"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
				"expected", nil,
				"got", err,
			)
		}
	}
}
