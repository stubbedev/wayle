package sysinfo

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Disk is one mounted filesystem from the sysinfo crate's disk list.
type Disk struct {
	MountPoint     string
	Filesystem     string
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
}

// ignoredFilesystems are the pseudo and special filesystems the sysinfo
// crate leaves out (tmpfs and network filesystems too: the Rust build
// has its linux-tmpfs and linux-netdevs features off).
var ignoredFilesystems = map[string]bool{
	"rootfs": true, "sysfs": true, "proc": true, "devtmpfs": true, "cgroup": true, "cgroup2": true,
	"pstore": true, "squashfs": true, "rpc_pipefs": true, "iso9660": true, "devpts": true,
	"hugetlbfs": true, "mqueue": true, "tmpfs": true, "cifs": true, "nfs": true, "nfs4": true,
}

// Disks reads /proc/mounts the way sysinfo's get_all_list does: the
// ignored filesystems and mount points under /sys, /proc, and /run
// (except /run/media) are skipped, one disk per mount point, and a
// filesystem statvfs reports as empty is left out.
func Disks() []Disk {
	data, err := os.ReadFile(filepath.Join(ProcRoot, "mounts"))
	if err != nil {
		return nil
	}
	var out []Disk
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		spec, file, fstype := fields[0], unescapeMount(fields[1]), fields[2]
		if ignoredFilesystems[fstype] || strings.HasPrefix(file, "/sys") || strings.HasPrefix(file, "/proc") ||
			(strings.HasPrefix(file, "/run") && !strings.HasPrefix(file, "/run/media")) || strings.HasPrefix(spec, "sunrpc") {
			continue
		}
		if seen[file] {
			continue
		}
		total, available, ok := statvfs(file)
		if !ok {
			continue
		}
		seen[file] = true
		out = append(out, Disk{MountPoint: file, Filesystem: fstype, TotalBytes: total, AvailableBytes: available, UsedBytes: satSub(total, available)})
	}
	return out
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\134`, `\`, `\040`, " ", `\011`, "\t", `\012`, "\n").Replace(s)
}

// statvfs is load_statvfs_values: block size times blocks and times
// available blocks; a zero total is no disk.
var statvfs = func(path string) (total, available uint64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := uint64(st.Bsize)
	total = bsize * st.Blocks
	if total == 0 {
		return 0, 0, false
	}
	return total, bsize * st.Bavail, true
}

// Storage is helpers.rs's StorageSnapshot: the mount points together.
type Storage struct {
	UsagePercent   float32
	UsedBytes      uint64
	TotalBytes     uint64
	AvailableBytes uint64
	// Filesystem is the one disk's type; Multiple marks several.
	Filesystem string
	Multiple   bool
}

// AggregateStorage is aggregate_storage: the disks whose mount point
// is one of paths exactly, summed; false when none is (or no path is
// given).
func AggregateStorage(disks []Disk, paths []string) (Storage, bool) {
	var matched []Disk
	for _, p := range paths {
		for _, d := range disks {
			if d.MountPoint == filepath.Clean(p) || d.MountPoint == p {
				matched = append(matched, d)
				break
			}
		}
	}
	if len(matched) == 0 {
		return Storage{}, false
	}
	var s Storage
	for _, d := range matched {
		s.UsedBytes += d.UsedBytes
		s.TotalBytes += d.TotalBytes
		s.AvailableBytes += d.AvailableBytes
	}
	s.UsagePercent = float32(s.UsedBytes) / float32(s.TotalBytes) * 100
	if len(matched) == 1 {
		s.Filesystem = matched[0].Filesystem
	} else {
		s.Multiple = true
	}
	return s, true
}
