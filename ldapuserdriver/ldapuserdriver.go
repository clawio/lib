package ldapuserdriver

import (
	"fmt"
)

import (
	"crypto/tls"
	"github.com/clawio/lib"
	"github.com/go-kit/kit/log/levels"
	"gopkg.in/ldap.v2"
)

type driver struct {
	logger       levels.Levels
	bindUsername string
	bindPassword string
	hostname     string
	port         int
	baseDN       string
	filter       string
}

func New(logger levels.Levels,
	bindUsername,
	bindPassword,
	hostname string,
	port int,
	baseDN string,
	filter string) (lib.UserDriver, error) {

	logger.Info().Log("msg", "ldap configuration", "hostname", hostname, "port", port, "bindusername", bindUsername, "basedn", baseDN, "filter", filter)
	return &driver{
		logger:       logger,
		bindUsername: bindUsername,
		bindPassword: bindPassword,
		hostname:     hostname,
		port:         port,
		baseDN:       baseDN,
		filter:       filter,
	}, nil
}

func (c *driver) GetByCredentials(username, password string) (lib.User, error) {
	l, err := ldap.DialTLS("tcp", fmt.Sprintf("%s:%d", c.hostname, c.port), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	defer l.Close()
	c.logger.Info().Log("msg", "connection stablished")

	// First bind with a read only user
	err = l.Bind(c.bindUsername, c.bindPassword)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}

	// Search for the given username
	searchRequest := ldap.NewSearchRequest(
		c.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf(c.filter, username),
		[]string{"dn"},
		nil,
	)

	sr, err := l.Search(searchRequest)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}

	if len(sr.Entries) != 1 {
		err := userNotFoundError("user " + username + " not found")
		c.logger.Error().Log("error", err)
		return nil, err
	}

	userdn := sr.Entries[0].DN
	c.logger.Info().Log("msg", "user exists", "dn", userdn)

	// Bind as the user to verify their password
	err = l.Bind(userdn, password)
	if err != nil {
		c.logger.Error().Log("error", err)
		return nil, err
	}
	c.logger.Info().Log("msg", "binding ok")

	// TODO(labkode) Get more attrs from LDAP query like email and displayName at least
	u := &user{
		username: username,
	}
	return u, nil
}

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
