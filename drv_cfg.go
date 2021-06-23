package goscaleio

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"encoding/hex"

	"github.com/google/uuid"
)

const (
	_IOCTLBase      = 'a'
	_IOCTLQueryGUID = 14
	_IOCTLQueryMDM  = 12
	_IOCTLRescan    = 10
	// IOCTLDevice is the default device to send queries to
	IOCTLDevice = "/dev/scini"
	mockGUID    = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	mockSystem  = "000000000001"
)

var (
	// SDCDevice is the device used to communicate with the SDC
	SDCDevice = IOCTLDevice
	// SCINIMockMode is used for testing upper layer code that attempts to call these methods
	SCINIMockMode = false
)

type ioctlGUID struct {
	rc         [8]byte
	uuid       [16]byte
	netIDMagic uint32
	netIDTime  uint32
}

// DrvCfgIsSDCInstalled will check to see if the SDC kernel module is loaded
func DrvCfgIsSDCInstalled() bool {
	if SCINIMockMode == true {
		return true
	}
	// Check to see if the SDC device is available
	info, err := os.Stat(SDCDevice)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DrvCfgQueryGUID will return the GUID of the locally installed SDC
func DrvCfgQueryGUID() (string, error) {
	if SCINIMockMode == true {
		return mockGUID, nil
	}
	f, err := os.Open(SDCDevice)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = f.Close()
	}()

	opCode := _IO(_IOCTLBase, _IOCTLQueryGUID)

	buf := [1]ioctlGUID{}
	// #nosec CWE-242, validated buffer is large enough to hold data
	err = ioctl(f.Fd(), opCode, uintptr(unsafe.Pointer(&buf[0])))

	if err != nil {
		return "", fmt.Errorf("QueryGUID error: %v", err)
	}

	rc, err := strconv.ParseInt(hex.EncodeToString(buf[0].rc[0:1]), 16, 64)
	if rc != 65 {
		return "", fmt.Errorf("Request to query GUID failed, RC=%d", rc)
	}

	g := hex.EncodeToString(buf[0].uuid[:len(buf[0].uuid)])
	u, err := uuid.Parse(g)
	discoveredGUID := strings.ToUpper(u.String())
	return discoveredGUID, nil
}

func DrvCfgQueryRescan() (string, error) {

	f, err := os.Open(SDCDevice)
	if err != nil {
		return "", fmt.Errorf("Powerflex SDC is not installed")
	}

	defer func() {
		_ = f.Close()
	}()

	opCode := _IO(_IOCTLBase, _IOCTLRescan)

	var rc int64
	// #nosec CWE-242, validated buffer is large enough to hold data
	err = ioctl(f.Fd(), opCode, uintptr(unsafe.Pointer(&rc)))

	if err != nil {
		return "", fmt.Errorf("Rescan error: %v", err)
	}
	rc_code := strconv.FormatInt(rc, 10)

	return rc_code, err
}

// internal, opaque to us, struct of IP addresses
type netAddress struct {
	opaque [24]byte
}

type ioctlMdmInfo struct {
	filler     [4]byte
	mdmIDL     uint32
	mdmIDH     uint32
	sdcIDL     uint32
	sdcIDH     uint32
	installIDL uint32
	installIDH uint32
	/*Total amount of socket addresses*/
	numSockAddrs uint64
	/*The MDM socket addresses*/
	addresses [16]netAddress
}

// ConfiguredCluster contains configuration information for one connected system
type ConfiguredCluster struct {
	// SystemID is the MDM cluster system ID
	SystemID string
	// SdcID is the ID of the SDC as known to the MDM cluster
	SdcID string
}

type ioctlQueryMDMs struct {
	rc      [8]byte
	numMdms uint16

	filler [4]byte //uint32
	/*Variable array of MDM. Its size is determined by numMdms*/
	mdms [20]ioctlMdmInfo
}

//DrvCfgQuerySystems will return the configured MDM endpoints for the locally installed SDC
func DrvCfgQuerySystems() (*[]ConfiguredCluster, error) {
	clusters := make([]ConfiguredCluster, 0)

	if SCINIMockMode == true {
		systemID := mockSystem
		sdcID := mockGUID
		aCluster := ConfiguredCluster{
			SystemID: systemID,
			SdcID:    sdcID,
		}
		clusters = append(clusters, aCluster)
		return &clusters, nil
	}

	f, err := os.Open(SDCDevice)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = f.Close()
	}()

	opCode := _IO(_IOCTLBase, _IOCTLQueryMDM)

	buf := ioctlQueryMDMs{}

	buf.numMdms = uint16(len(buf.mdms))

	// #nosec CWE-242, validated buffer is large enough to hold data
	err = ioctl(f.Fd(), opCode, uintptr(unsafe.Pointer(&buf)))

	if err != nil {
		return nil, fmt.Errorf("queryMDM error: %v", err)
	}

	rc, err := strconv.ParseInt(hex.EncodeToString(buf.rc[0:1]), 16, 64)
	if rc != 65 {
		return nil, fmt.Errorf("Request to query MDM failed, RC=%d", rc)
	}

	for i := uint16(0); i < buf.numMdms; i++ {
		systemID := fmt.Sprintf("%8.8x%8.8x",
			buf.mdms[i].mdmIDH, buf.mdms[i].mdmIDL)
		sdcID := fmt.Sprintf("%8.8x%8.8x",
			buf.mdms[i].sdcIDH, buf.mdms[i].sdcIDL)
		aCluster := ConfiguredCluster{
			SystemID: systemID,
			SdcID:    sdcID,
		}
		clusters = append(clusters, aCluster)
	}

	return &clusters, nil
}

func ioctl(fd, op, arg uintptr) error {
	_, _, ep := syscall.Syscall(syscall.SYS_IOCTL, fd, op, arg)
	if ep != 0 {
		return syscall.Errno(ep)
	}
	return nil
}

func _IO(t uintptr, nr uintptr) uintptr {
	return _IOC(0x0, t, nr, 0)
}

func _IOC(dir, t, nr, size uintptr) uintptr {
	return (dir << 30) | (t << 8) | nr | (size << 16)
}
