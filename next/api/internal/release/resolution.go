package release

import (
	"errors"
	"net/netip"
)

var (
	ErrInvalidIP = errors.New("one IPv4 address is required")
	ErrNoSwitch  = errors.New("no enabled validated switch owns this IP")
	ErrAmbiguous = errors.New("multiple equally specific switches own this IP")
)

type Network struct {
	ID, SwitchID, CIDR                string
	Enabled, Validated, SwitchEnabled bool
}

func ResolveNetwork(address string, networks []Network) (Network, error) {
	ip, err := netip.ParseAddr(address)
	if err != nil || !ip.Is4() || address != ip.String() {
		return Network{}, ErrInvalidIP
	}
	var selected Network
	best := -1
	ambiguous := false
	for _, network := range networks {
		if !network.Enabled || !network.Validated || !network.SwitchEnabled {
			continue
		}
		prefix, err := netip.ParsePrefix(network.CIDR)
		if err != nil || !prefix.Addr().Is4() || !prefix.Contains(ip) {
			continue
		}
		if prefix.Bits() > best {
			best, selected, ambiguous = prefix.Bits(), network, false
		} else if prefix.Bits() == best && network.SwitchID != selected.SwitchID {
			ambiguous = true
		}
	}
	if best < 0 {
		return Network{}, ErrNoSwitch
	}
	if ambiguous {
		return Network{}, ErrAmbiguous
	}
	return selected, nil
}
