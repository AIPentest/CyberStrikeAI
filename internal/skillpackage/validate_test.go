package skillpackage

import (
	"strings"
	"testing"
)

func TestValidateSkillMDPackage(t *testing.T) {
	t.Parallel()

	const valid = "---\nname: report-summary\ndescription: Summarize a synthetic report.\n---\n\n# Instructions\n\nSummarize the supplied report.\n"
	tests := []struct {
		name    string
		raw     string
		dirName string
		wantErr string
	}{
		{name: "valid", raw: valid},
		{name: "UTF-8 BOM", raw: "\ufeff" + valid},
		{name: "CRLF line endings", raw: strings.ReplaceAll(valid, "\n", "\r\n")},
		{
			name: "optional fields",
			raw:  strings.Replace(valid, "description:", "license: Apache-2.0\ncompatibility: Requires Go.\nmetadata:\n  author: example-org\n  version: '1.0'\nallowed-tools: Read\ndescription:", 1),
		},
		{name: "empty file", wantErr: "SKILL.md is empty"},
		{name: "whitespace file", raw: " \n\t\n", wantErr: "SKILL.md is empty"},
		{name: "missing front matter", raw: "# Instructions\n", wantErr: "must start with YAML front matter"},
		{name: "unclosed front matter", raw: "---\nname: report-summary\n", wantErr: "front matter must end"},
		{
			name:    "malformed YAML",
			raw:     "---\nname: [\n---\nInstructions\n",
			wantErr: "SKILL.md front matter:",
		},
		{
			name:    "duplicate YAML key",
			raw:     strings.Replace(valid, "name:", "name: other-name\nname:", 1),
			wantErr: "already defined",
		},
		{
			name:    "unsupported top-level key",
			raw:     strings.Replace(valid, "description:", "custom-field: value\ndescription:", 1),
			wantErr: "unsupported key",
		},
		{
			name:    "missing name",
			raw:     strings.Replace(valid, "name: report-summary\n", "", 1),
			wantErr: "name is required",
		},
		{
			name:    "whitespace name",
			raw:     strings.Replace(valid, "name: report-summary", "name: '   '", 1),
			wantErr: "name is required",
		},
		{
			name:    "missing description",
			raw:     strings.Replace(valid, "description: Summarize a synthetic report.\n", "", 1),
			wantErr: "description is required",
		},
		{
			name:    "whitespace description",
			raw:     strings.Replace(valid, "Summarize a synthetic report.", "'   '", 1),
			wantErr: "description is required",
		},
		{
			name:    "empty body",
			raw:     "---\nname: report-summary\ndescription: Summarize a report.\n---\n \t\n",
			wantErr: "markdown body after front matter must not be empty",
		},
		{
			name:    "directory mismatch",
			raw:     valid,
			dirName: "other-name",
			wantErr: "must match directory name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dirName := tt.dirName
			if dirName == "" {
				dirName = "report-summary"
			}
			err := ValidateSkillMDPackage([]byte(tt.raw), dirName)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSkillMDPackage() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSkillMDPackage() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSkillMDPackageFieldLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{name: "name at limit", field: "name", value: strings.Repeat("a", 64)},
		{name: "name over limit", field: "name", value: strings.Repeat("a", 65), wantErr: true},
		{name: "name with digits", field: "name", value: "report-v2"},
		{name: "uppercase name", field: "name", value: "Report", wantErr: true},
		{name: "name with underscore", field: "name", value: "report_summary", wantErr: true},
		{name: "leading hyphen", field: "name", value: "-report", wantErr: true},
		{name: "trailing hyphen", field: "name", value: "report-", wantErr: true},
		{name: "consecutive hyphens", field: "name", value: "report--summary", wantErr: true},
		{name: "description at limit", field: "description", value: strings.Repeat("a", 1024)},
		{name: "description over limit", field: "description", value: strings.Repeat("a", 1025), wantErr: true},
		{name: "Unicode description at limit", field: "description", value: strings.Repeat("界", 1024)},
		{name: "Unicode description over limit", field: "description", value: strings.Repeat("界", 1025), wantErr: true},
		{name: "compatibility at limit", field: "compatibility", value: strings.Repeat("a", 500)},
		{name: "compatibility over limit", field: "compatibility", value: strings.Repeat("a", 501), wantErr: true},
		{name: "Unicode compatibility at limit", field: "compatibility", value: strings.Repeat("界", 500)},
		{name: "Unicode compatibility over limit", field: "compatibility", value: strings.Repeat("界", 501), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fields := map[string]string{
				"name":          "report-summary",
				"description":   "Summarize a synthetic report.",
				"compatibility": "Requires Go.",
			}
			fields[tt.field] = tt.value
			raw := "---\nname: " + fields["name"] + "\ndescription: " + fields["description"] +
				"\ncompatibility: " + fields["compatibility"] + "\n---\n\n# Instructions\n"
			err := ValidateSkillMDPackage([]byte(raw), fields["name"])
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSkillMDPackage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
