package memuserdriver

import (
	"fmt"
	"github.com/clawio/lib"
	"strings"
)

func New(userList string) lib.UserDriver {
	users := []*user{}
	userTokens := strings.Split(userList, ",")
	for _, userToken := range userTokens {
		fields := strings.Split(userToken, ":")
		if len(fields) >= 4 {
			users = append(users, &user{
				username:    fields[0],
				password:    fields[1],
				email:       fields[2],
				displayName: fields[3],
			})
		}
	}
	return &driver{users: users}
}

func (c *driver) GetByCredentials(username, password string) (lib.User, error) {
	for _, u := range c.users {
		if u.username == username && u.password == password {
			return u, nil
		}
	}
	return nil, userNotFoundError(fmt.Sprintf("user with credentials %s:xxxx not found", username))
}

type user struct {
	username    string
	email       string
	displayName string
	password    string
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

type driver struct {
	users []*user
}

type userNotFoundError string

func (e userNotFoundError) Error() string {
	return string(e)
}
func (e userNotFoundError) Code() lib.Code {
	return lib.Code(lib.CodeUserNotFound)
}
func (e userNotFoundError) Message() string {
	return string(e)
}
