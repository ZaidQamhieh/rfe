package files

// Drive is one mounted volume (openapi schema Drive).
type Drive struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	TotalBytes int64  `json:"totalBytes"`
	FreeBytes  int64  `json:"freeBytes"`
	IsOS       bool   `json:"isOS"`
}

// drivesFn lists every volume of the machine; a variable so tests can supply a
// fixed list.
var drivesFn = platformDrives

// Drives lists the volumes this Service can reach: with an unrestricted Set
// every volume, otherwise only those at or inside one of the allowed folders. A
// volume that merely contains an allowed folder is left out, since nothing on
// it outside the folder can be browsed and listing it would only show that it
// exists.
func (s *Service) Drives() ([]Drive, error) {
	all, err := drivesFn()
	if err != nil {
		return nil, err
	}
	out := make([]Drive, 0, len(all))
	for _, d := range all {
		if s.sees(d.Path) {
			out = append(out, d)
		}
	}
	return out, nil
}
