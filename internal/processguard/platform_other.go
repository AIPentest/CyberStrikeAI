//go:build !linux && !windows

package processguard

import (
	"encoding/json"
	"fmt"
)

func configurePlatform(o *Options) error {
	if o.Mode == "required" || o.CgroupRoot != "" {
		return fmt.Errorf("kernel task containment is unavailable on this OS; use a Linux cgroup deployment")
	}
	return nil
}
func newPlatformGroup(id string, o Options) (Group, error) {
	if err := configurePlatform(&o); err != nil {
		return nil, err
	}
	return newUnixGroup()
}
func guardianMain(dec *json.Decoder, enc *json.Encoder) error {
	var req watchRequest
	if err := dec.Decode(&req); err != nil {
		return err
	}
	if req.Name != "process_group" {
		return fmt.Errorf("unsupported guardian")
	}
	return groupGuardian(dec, enc)
}
