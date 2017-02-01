package etcdregistrydriver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/clawio/clawiod/root"
	"github.com/coreos/etcd/client"
	"github.com/go-kit/kit/log/levels"
	"strings"
	"time"
)

const (
	ttl        = time.Second * 10
	defaultKey = "/nodes"
)

type driver struct {
	logger   levels.Levels
	urls     []string
	key      string
	username string
	password string
	client   client.Client
}

func New(logger levels.Levels, urlList, key, username, password string) (root.RegistryDriver, error) {
	urls := strings.Split(urlList, ",")
	etcdConfig := client.Config{
		Endpoints:               urls,
		Username:                username,
		Password:                password,
		Transport:               client.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	}
	c, err := client.New(etcdConfig)
	if err != nil {
		return nil, err
	}

	if key == "" {
		key = defaultKey
	}

	r := &driver{
		logger:   logger,
		urls:     etcdConfig.Endpoints,
		key:      key,
		username: etcdConfig.Username,
		password: etcdConfig.Password,
		client:   c,
	}
	return r, nil
}
func (d *driver) Register(ctx context.Context, node root.RegistryNode) error {
	kapi := client.NewKeysAPI(d.client)
	key := fmt.Sprintf("%s/%s/%s", d.key, node.Rol(), node.ID())
	n := &xnode{
		Xhost:    node.Host(),
		Xid:      node.ID(),
		Xurl:     node.URL(),
		Xversion: node.Version(),
	}
	jsonValue, err := json.Marshal(n)
	if err != nil {
		return err
	}

	_, err = kapi.Set(ctx, key, string(jsonValue), &client.SetOptions{TTL: ttl})
	if err != nil {
		d.logger.Error().Log("error", err)
		return err
	}
	d.logger.Info().Log("msg", "node registered", "key", key, "nodeid", node.ID(), "noderol", node.Rol(), "nodeurl", node.URL(), "nodeversion", node.Version(), "nodehost", node.Host())
	return nil
}

func (d *driver) GetNodesForRol(ctx context.Context, rol string) ([]root.RegistryNode, error) {
	kapi := client.NewKeysAPI(d.client)
	key := fmt.Sprintf("%s/%s", d.key, rol)
	resp, err := kapi.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}

	stringNodes := []string{}
	nodes := []root.RegistryNode{}
	for _, n := range resp.Node.Nodes {
		rn := &xnode{}
		if err := json.Unmarshal([]byte(n.Value), rn); err == nil {
			nodes = append(nodes, rn)
			stringNodes = append(stringNodes, rn.URL())
		}
	}

	d.logger.Info().Log("msg", "got nodes for rol", "rol", rol, "numnodes", len(nodes), "nodes", strings.Join(stringNodes, ","))
	return nodes, nil
}

type xnode struct {
	Xid      string `json:"id"`
	Xrol     string `json:"rol"`
	Xhost    string `json:"host"`
	Xversion string `json:"version"`
	Xurl     string `json:"url"`
}

func (n *xnode) ID() string {
	return n.Xid
}
func (n *xnode) Rol() string {
	return n.Xrol
}
func (n *xnode) Host() string {
	return n.Xhost
}
func (n *xnode) Version() string {
	return n.Xversion
}
func (n *xnode) URL() string {
	return n.Xurl
}
