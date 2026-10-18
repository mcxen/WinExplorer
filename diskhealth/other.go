//go:build !darwin && !linux && !windows

package diskhealth

// probe has nothing to read on this platform: no sysfs, no diskutil, no
// Storage cmdlets. The page says so rather than showing an empty list.
func probe() Report {
	return Report{Notes: []string{"此平台暂不支持磁盘健康探测"}}
}

// platformMounts has no mount table here.
func platformMounts() ([]mountEntry, error) { return nil, errNotSupported }

// capacityOf cannot measure a volume here.
func capacityOf(string) (free, total int64, ok bool) { return 0, 0, false }
