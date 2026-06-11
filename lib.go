package doorscaddy

import "github.com/doors-dev/doors-caddy/lib"

var ErrorBase64 = lib.ErrorBase64
var ErrorCipher = lib.ErrorCipher
var ErrorParse = lib.ErrorParse

type TokenCipher = lib.TokenCipher

func NewTokenCipher(secret string) (TokenCipher, error) {
	return lib.NewTokenCipher(secret)
}
