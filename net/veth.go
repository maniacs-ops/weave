package net

import (
	"fmt"

	"github.com/vishvananda/netlink"

	"github.com/weaveworks/weave/common/odp"
)

// create and attach local name to the Weave bridge
func CreateAndAttachVeth(localName, peerName, bridgeName string, mtu int) (*netlink.Veth, error) {
	maybeBridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, fmt.Errorf(`bridge "%s" not present; did you launch weave?`, bridgeName)
	}

	if mtu == 0 {
		mtu = maybeBridge.Attrs().MTU
	}
	local := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: localName,
			MTU:  mtu},
		PeerName: peerName,
	}
	if err := netlink.LinkAdd(local); err != nil {
		return nil, fmt.Errorf(`could not create veth pair %s-%s: %s`, local.Name, local.PeerName, err)
	}

	switch maybeBridge.(type) {
	case *netlink.Bridge:
		if err := netlink.LinkSetMasterByIndex(local, maybeBridge.Attrs().Index); err != nil {
			return nil, fmt.Errorf(`unable to set master of %s: %s`, local.Name, err)
		}
	case *netlink.GenericLink:
		if maybeBridge.Type() != "openvswitch" {
			return nil, fmt.Errorf(`device "%s" is of type "%s"`, bridgeName, maybeBridge.Type())
		}
		if err := odp.AddDatapathInterface(bridgeName, local.Name); err != nil {
			return nil, fmt.Errorf(`failed to attach %s to device "%s": %s`, local.Name, bridgeName, err)
		}
	case *netlink.Device:
		// Assume it's our openvswitch device, and the kernel has not been updated to report the kind.
		if err := odp.AddDatapathInterface(bridgeName, local.Name); err != nil {
			return nil, fmt.Errorf(`failed to attach %s to device "%s": %s`, local.Name, bridgeName, err)
		}
	default:
		return nil, fmt.Errorf(`device "%s" is not a bridge`, bridgeName)
	}

	if err := netlink.LinkSetUp(local); err != nil {
		return nil, fmt.Errorf("unable to bring veth up: %s", err)
	}

	return local, nil
}