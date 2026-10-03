package shortener

import "math/rand/v2"

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenCode returns a string with randomized alphanumeric characters with size length
func GenCode(length int) string {
	code := make([]byte, length)
	for i := range length {
		posIndex := rand.IntN(len(alphabet))
		code[i] = alphabet[posIndex]
	}
	return string(code)
}
