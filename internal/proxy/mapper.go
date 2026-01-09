package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/huin/goupnp/dcps/internetgateway2"
	"golang.org/x/sync/errgroup"
)

// Shamelessly stolen from the guide on the github.
type routerClient interface {
	AddPortMapping(
		NewRemoteHost string,
		NewExternalPort uint16,
		NewProtocol string,
		NewInternalPort uint16,
		NewInternalClient string,
		NewEnabled bool,
		NewPortMappingDescription string,
		NewLeaseDuration uint32,
	) (err error)

	GetExternalIPAddress() (
		NewExternalIPAddress string,
		err error,
	)
}

type PortMapper struct {
	conn routerClient
}

func TryMapper(ctx context.Context) (*PortMapper, error) {
	tasks, _ := errgroup.WithContext(ctx)
	// Request each type of client in parallel, and return what is found.
	var ip1Clients []*internetgateway2.WANIPConnection1
	tasks.Go(func() error {
		var err error
		ip1Clients, _, err = internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
		return err
	})
	var ip2Clients []*internetgateway2.WANIPConnection2
	tasks.Go(func() error {
		var err error
		ip2Clients, _, err = internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
		return err
	})
	var ppp1Clients []*internetgateway2.WANPPPConnection1
	tasks.Go(func() error {
		var err error
		ppp1Clients, _, err = internetgateway2.NewWANPPPConnection1ClientsCtx(ctx)
		return err
	})

	if err := tasks.Wait(); err != nil {
		return nil, err
	}

	var client routerClient
	// There are some pretty nasty assumptions here that are most likely correct but not always:
	// 1. There is only a single client: this won't be true for some complicated setups
	// 2. The server only has a single network interface that has UPnP available.
	// The second one becomes an issue during TryMap
	switch {
	case len(ip2Clients) == 1:
		client = ip2Clients[0]
	case len(ip1Clients) == 1:
		client = ip1Clients[0]
	case len(ppp1Clients) == 1:
		client = ppp1Clients[0]
	default:
		return nil, errors.New("multiple or no services found")
	}

	return &PortMapper{
		conn: client,
	}, nil
}

// Get the address of the first interface that reports one
// hacky assuming there is only one
func yoinkFirstIntfAddr() (string, error) {
	intfs, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, intf := range intfs {
		addrs, err := intf.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			return addr.String(), nil
		}
	}

	return "", fmt.Errorf("Couldn't enumerate IP from interfaces")
}

func (pm *PortMapper) TryMap(addr net.Addr) error {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("Host IP invalid: %s", host)
	}

	// Check if they bound all interfaces
	if ip.IsUnspecified() {
		// Assume: only a single network interface
		host, err = yoinkFirstIntfAddr()
		if err != nil {
			return err
		}
	}

	pval, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("Port number parse failed? %v", err)
	}

	return pm.conn.AddPortMapping(
		"",
		uint16(pval),
		"TCP",
		uint16(pval),
		host,
		true,
		"DiscoPanel",
		3600,
	)
}
