package service

import (
	"fmt"
	"unicode"
)

func ValidateStrongPassword(password string) error {
	if len(password) < 10 {
		return fmt.Errorf("la contraseña debe tener al menos 10 caracteres")
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSymbol {
		return fmt.Errorf("usa una contraseña fuerte con mayúscula, minúscula, número y símbolo")
	}
	return nil
}
