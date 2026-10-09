package broker

import (
	"errors"
	"net"
	"sort"
)

type localAddress struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
	Version   int    `json:"version"`
	Loopback  bool   `json:"loopback"`
}

// localAddresses returns every unicast address on an enabled interface. The
// default-route IPv4 address is sorted first because it is usually the useful
// choice when VPN and virtual adapters are also present.
func localAddresses() ([]localAddress, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	preferred, _ := reachableLocalIP("1.1.1.1")
	seen := map[string]bool{}
	result := []localAddress{}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || seen[ip.String()] {
				continue
			}
			version := 6
			if ip.To4() != nil {
				version = 4
			}
			seen[ip.String()] = true
			result = append(result, localAddress{IP: ip.String(), Interface: networkInterface.Name, Version: version, Loopback: ip.IsLoopback()})
		}
	}
	if len(result) == 0 {
		return nil, errors.New("未找到已启用网卡的 IP 地址")
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if (a.IP == preferred) != (b.IP == preferred) {
			return a.IP == preferred
		}
		if a.Loopback != b.Loopback {
			return !a.Loopback
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if a.Interface != b.Interface {
			return a.Interface < b.Interface
		}
		return a.IP < b.IP
	})
	return result, nil
}

func defaultLocalIP(addresses []localAddress) string {
	for _, address := range addresses {
		if !address.Loopback {
			return address.IP
		}
	}
	if len(addresses) > 0 {
		return addresses[0].IP
	}
	return ""
}

func selectLocalIP(selected string) (string, []localAddress, error) {
	addresses, err := localAddresses()
	if err != nil {
		return "", nil, err
	}
	if selected == "" {
		return defaultLocalIP(addresses), addresses, nil
	}
	parsed := net.ParseIP(selected)
	if parsed == nil {
		return "", addresses, errors.New("请选择列表中的本机 IP")
	}
	selected = parsed.String()
	for _, address := range addresses {
		if address.IP == selected {
			return selected, addresses, nil
		}
	}
	return "", addresses, errors.New("所选 IP 已不属于本机，请刷新后重选")
}

// reachableLocalIP asks the kernel which local address it would use to reach
// peer. UDP connect performs route selection without sending a packet.
func reachableLocalIP(peer string) (string, error) {
	ip := net.ParseIP(peer)
	if ip == nil {
		return "", errors.New("目标 IP 无效")
	}
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: ip, Port: 9})
	if err != nil {
		return "", errors.New("系统中没有可用路由")
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil || local.IP.IsUnspecified() {
		return "", errors.New("系统未选择有效的本机地址")
	}
	return local.IP.String(), nil
}
