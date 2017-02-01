package jwttokendriver

import (
	"time"

	"github.com/clawio/lib"
	"github.com/dgrijalva/jwt-go"
	"github.com/go-kit/kit/log/levels"
)

type authenticator struct {
	key    string
	cm     lib.ContextManager
	logger levels.Levels
}

func New(key string, cm lib.ContextManager, logger levels.Levels) lib.TokenDriver {
	logger = logger.With("pkg", "jwttokendriver")
	return &authenticator{key: key, cm: cm, logger: logger}
}

func (a *authenticator) CreateToken(user lib.User) (string, error) {
	if user == nil {
		return "", badUserError("user is <nil>") }
	token := jwt.New(jwt.GetSigningMethod("HS256"))
	claims := token.Claims.(jwt.MapClaims)
	claims["username"] = user.Username()
	claims["email"] = user.Email()
	claims["display_name"] = user.DisplayName()
	claims["exp"] = time.Now().Add(time.Second * 3600).UnixNano()
	return token.SignedString([]byte(a.key))
}

func (a *authenticator) UserFromToken(token string) (lib.User, error) {
	rawToken, err := a.parseToken(token)
	if err != nil {
		a.logger.Error().Log("error", err)
		return nil, err
	}
	return a.getUserFromRawToken(rawToken)
}

func (a *authenticator) getUserFromRawToken(rawToken *jwt.Token) (lib.User, error) {
	claims := rawToken.Claims.(jwt.MapClaims)
	username, ok := claims["username"].(string)
	if !ok {
		err := invalidTokenError("username claim failed cast to string")
		a.logger.Error().Log("error", err)
		return nil, err
	}

	email, ok := claims["email"].(string)
	if !ok {
		err := invalidTokenError("email claim failed cast to string")
		a.logger.Error().Log("error", err)
		return nil, err
	}

	displayName, ok := claims["display_name"].(string)
	if !ok {
		err := invalidTokenError("display_name claim failed cast to string")
		a.logger.Error().Log("error", err)
		return nil, err
	}
	return &user{
		username:    username,
		email:       email,
		displayName: displayName,
	}, nil
}

func (a *authenticator) parseToken(token string) (*jwt.Token, error) {
	return jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return []byte(a.key), nil
	})
}


type badUserError string

func (e badUserError) Error() string {
	return string(e)
}
func (e badUserError) Code() lib.Code {
	return lib.Code(lib.CodeBadAuthenticationData)
}
func (e badUserError) Message() string {
	return string(e)
}

type invalidTokenError string

func (e invalidTokenError) Error() string {
	return string(e)
}
func (e invalidTokenError) Code() lib.Code {
	return lib.Code(lib.CodeBadAuthenticationData)
}
func (e invalidTokenError) Message() string {
	return string(e)
}

// user represents an in-memory user.
type user struct {
	username    string
	email       string
	displayName string
}

func (u *user) Username() string {
	return u.username
}

func (u *user) Email() string {
	return u.email
}

func (u *user) DisplayName() string {
	return u.displayName
}

func (u *user) ExtraAttributes() map[string]interface{} {
	return nil
}
