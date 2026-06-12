package doorscaddy

import "github.com/doors-dev/doors-caddy/common"

var ErrorBase64 = common.ErrorBase64
var ErrorCipher = common.ErrorCipher
var ErrorParse = common.ErrorParse

type TokenCipher = common.TokenCipher

func NewTokenCipher(secret string) (TokenCipher, error) {
	return common.NewTokenCipher(secret)
}
