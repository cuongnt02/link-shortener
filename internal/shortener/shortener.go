package shortener

import (
	"errors"
	"math/rand/v2"
)

var ErrInvalidCodeLength = errors.New("invalid code length supplied")

// alphabet defines the possible alphanumeric characters that could be generated
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenCode returns a string with randomized alphanumeric characters with size length
func GenCode(length int) (string, error) {
	if length <= 0 {
		return "", ErrInvalidCodeLength
	}
	code := make([]byte, length)
	for i := range length {
		posIndex := rand.IntN(len(alphabet))
		code[i] = alphabet[posIndex]
	}
	return string(code), nil
}
