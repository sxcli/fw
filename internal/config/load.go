package config

import "fmt"

// PeekCore is the first pipeline pass: it leniently extracts the core's
// own configuration from environment and arguments only — no file can be
// located before --config is known. File-sourced core values arrive
// later via Files.ApplyCore.
func PeekCore(appletID string, src Sources) (Core, []error) {
	var core Core
	sch, errs := NewSchema(appletID, &core, nil)
	if len(errs) == 0 {
		errs = append(errs, sch.applyEnv(src.LookupEnv)...)
		_, aerrs := sch.parseArgs(src.Args, true)
		errs = append(errs, aerrs...)
	}
	return core, errs
}

// ApplyCore refills a fresh Core in full precedence order once the
// files are loaded: file sections, then environment, then arguments.
// This is the Core the closure resolution must use — a disable/enable/
// override list in a config file is only visible here.
func (f *Files) ApplyCore(appletID string, src Sources) (Core, []error) {
	var core Core
	sch, errs := NewSchema(appletID, &core, nil)
	if len(errs) == 0 {
		errs = append(errs, sch.applyFiles(f)...)
		errs = append(errs, sch.applyEnv(src.LookupEnv)...)
		_, aerrs := sch.parseArgs(src.Args, true)
		errs = append(errs, aerrs...)
	}
	return core, errs
}

// Apply is the strict pipeline pass over the full schema: files, then
// environment, then arguments, unknown argument = error. It fills every
// member's config struct in place and returns the trailing positionals.
func (s *Schema) Apply(files *Files, src Sources) (Loaded, []error) {
	var errs []error
	errs = append(errs, s.applyFiles(files)...)
	errs = append(errs, s.applyEnv(src.LookupEnv)...)
	positionals, aerrs := s.parseArgs(src.Args, false)
	errs = append(errs, aerrs...)
	return Loaded{Positionals: positionals}, errs
}

// applyEnv writes every present environment variable into its field.
// Slice values are comma-separated and replace the field whole.
func (s *Schema) applyEnv(lookup func(string) (string, bool)) []error {
	var errs []error
	if lookup != nil {
		for _, svc := range s.services {
			for _, f := range svc.fields {
				if f.EnvName != "" {
					if value, present := lookup(f.EnvName); present {
						target := svc.cfg.Elem().FieldByIndex(f.Path)
						var err error
						if f.IsSlice {
							err = setSliceFromEnv(target, value)
						} else {
							err = setFromString(target, value)
						}
						if err != nil {
							errs = append(errs, fmt.Errorf("$%s: %v", f.EnvName, err))
						}
					}
				}
			}
		}
	}
	return errs
}
