package odp

import (
	"fmt"
	"net"
	"syscall"

	"github.com/weaveworks/go-odp/odp"

	wnet "github.com/weaveworks/weave/common/net"
)

// ODP admin functionality

func CreateDatapath(dpname string, mtuToCheck int) (supported bool, validMTU bool, err error) {
	validMTU = true

	dpif, err := odp.NewDpif()
	if err != nil {
		if odp.IsKernelLacksODPError(err) {
			return false, validMTU, nil
		}
		return true, validMTU, err
	}
	defer dpif.Close()

	dp, err := dpif.CreateDatapath(dpname)
	if err != nil && !odp.IsDatapathNameAlreadyExistsError(err) {
		return true, validMTU, err
	}

	var (
		vpid   odp.VportID
		vpname string
	)
	if vpid, vpname, err = createDummyVxlanVport(dp); err != nil {
		if nlerr, ok := err.(odp.NetlinkError); ok {
			if syscall.Errno(nlerr) == syscall.EAFNOSUPPORT {
				dp.Delete()
				return false, validMTU, fmt.Errorf("kernel does not have Open vSwitch VXLAN support")
			}
		}
	}

	// Check whether the user is exposed to https://github.com/weaveworks/weave/issues/1853
	if mtuToCheck > 0 {
		// Create vxlan vport if the previous attempt has failed. Retry a few
		// times if the creation fails due to the chosen vxlan UDP port being occupied.
		for i := 0; i < 5 && !(err == nil && vpname == ""); i++ {
			if vpid, vpname, err = createDummyVxlanVport(dp); err != nil {
				if errno, ok := err.(syscall.Errno); !(ok && errno == syscall.EADDRINUSE) {
					// Skip the check if something went wrong
					return true, validMTU, nil
				}
			} else {
				break
			}
		}
		// Couldn't create the vport, skip the check
		if err != nil {
			return true, validMTU, err
		}
		validMTU = checkMTU(vpname, mtuToCheck)
	}

	if err == nil && vpname != "" {
		dp.DeleteVport(vpid)
	}

	return true, validMTU, nil
}

func DeleteDatapath(dpname string) error {
	dpif, err := odp.NewDpif()
	if err != nil {
		return err
	}
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(dpname)
	if err != nil {
		if odp.IsNoSuchDatapathError(err) {
			return nil
		}
		return err
	}

	return dp.Delete()
}

func AddDatapathInterface(dpname string, ifname string) error {
	dpif, err := odp.NewDpif()
	if err != nil {
		return err
	}
	defer dpif.Close()

	dp, err := dpif.LookupDatapath(dpname)
	if err != nil {
		return err
	}

	_, err = dp.CreateVport(odp.NewNetdevVportSpec(ifname))
	return err
}

func createDummyVxlanVport(dp odp.DatapathHandle) (odp.VportID, string, error) {
	// A dummy way to get an ephemeral port for UDP
	udpconn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return 0, "", err
	}
	portno := uint16(udpconn.LocalAddr().(*net.UDPAddr).Port)
	udpconn.Close()

	vpname := fmt.Sprintf("vxlan-%d", portno)
	vpid, err := dp.CreateVport(odp.NewVxlanVportSpec(vpname, portno))

	return vpid, vpname, err
}

func checkMTU(vpname string, mtuToCheck int) bool {
	// Setting >1500 MTU will fail with EINVAL, if the user is affected by
	// the kernel issue.
	if err := wnet.SetMTU(vpname, mtuToCheck); err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.EINVAL {
			return false
		}
		// NB: If no link interface for the vport is found (which
		// might be a case for SetMTU to fail), the user is probably
		// running the <= 4.2 kernel, which is fine.
	}
	return true
}
