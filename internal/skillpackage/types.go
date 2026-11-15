// Package skillpackage provides filesystem-backed Agent Skills layout (SKILL.md + package files)
// for HTTP admin APIs. Runtime discovery and progressive loading for agents use Pi skill middleware.
package skillpackage

// SkillManifest is parsed from SKILL.md front matter (https://agentskills.io/specification.md).
type SkillManifest struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	License       string         `yaml:"license,omitempty"`
	Compatibility string         `yaml:"compatibility,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
	AllowedTools  string         `yaml:"allowed-tools,omitempty"`
}

// SkillSummary is API metadata for one skill directory.
type SkillSummary struct {
	ID          string   `json:"id"`
	DirName     string   `json:"dir_name"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Path        string   `json:"path"`
	Tags        []string `json:"tags"`
	Triggers    []string `json:"triggers,omitempty"`
	ScriptCount int      `json:"script_count"`
	FileCount   int      `json:"file_count"`
	FileSize    int64    `json:"file_size"`
	ModTime     string   `json:"mod_time"`
	Progressive bool     `json:"progressive"`
}

// SkillScriptInfo describes a file under scripts/.
type SkillScriptInfo struct {
	Name        string `json:"name"`
	RelPath     string `json:"rel_path"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size"`
}

// SkillSection is derived from ## headings in SKILL.md.
type SkillSection struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Heading string `json:"heading"`
	Level   int    `json:"level"`
}

// PackageFileInfo describes one file inside a package.
type PackageFileInfo struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir,omitempty"`
}

// SkillView is a loaded package for admin / API.
type SkillView struct {
	DirName      string            `json:"dir_name"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Content      string            `json:"content"`
	Path         string            `json:"path"`
	Version      string            `json:"version"`
	Tags         []string          `json:"tags"`
	Scripts      []SkillScriptInfo `json:"scripts,omitempty"`
	Sections     []SkillSection    `json:"sections,omitempty"`
	PackageFiles []PackageFileInfo `json:"package_files,omitempty"`
}
