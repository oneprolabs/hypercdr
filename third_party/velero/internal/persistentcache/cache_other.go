//go:build !linux

package persistentcache

import "fmt"

const Root = "/var/cache/hypercdr"

func DataRoot() string { return Root }

type Lease struct{ Path string }

func Acquire(root, identity string, budget int64) (*Lease, error) {
	return nil, fmt.Errorf("persistent cache requires Linux")
}
func (lease *Lease) Close()   {}
func Clean(root string) error { return fmt.Errorf("persistent cache requires Linux") }
