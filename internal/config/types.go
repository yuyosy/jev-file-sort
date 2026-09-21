package config

import "time"

type Config struct {
	Version       int           `yaml:"version" json:"version"`
	Mode          string        `yaml:"mode,omitempty" json:"mode"`
	Selection     Selection     `yaml:"selection,omitempty" json:"selection"`
	Scan          Scan          `yaml:"scan,omitempty" json:"scan"`
	Folders       Folders       `yaml:"folders,omitempty" json:"folders"`
	Output        Output        `yaml:"output,omitempty" json:"output"`
	Uncategorized Uncategorized `yaml:"uncategorized,omitempty" json:"uncategorized"`
	Jev           Jev           `yaml:"jev,omitempty" json:"jev"`
	History       History       `yaml:"history,omitempty" json:"history"`
	Categories    []Category    `yaml:"categories,omitempty" json:"categories"`
	Rules         []Rule        `yaml:"rules,omitempty" json:"rules"`
	SourceFiles   []string      `yaml:"-" json:"source_files,omitempty"`
	LoadedAt      time.Time     `yaml:"-" json:"loaded_at"`
}

type Selection struct {
	Include []string `yaml:"include,omitempty" json:"include"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude"`
}

type Scan struct {
	Recursive       *bool `yaml:"recursive,omitempty" json:"recursive"`
	MaxDepth        *int  `yaml:"max_depth,omitempty" json:"max_depth,omitempty"`
	IncludeDotfiles *bool `yaml:"include_dotfiles,omitempty" json:"include_dotfiles"`
	FollowSymlinks  *bool `yaml:"follow_symlinks,omitempty" json:"follow_symlinks"`
}

type Folders struct {
	RulesEnabled *bool `yaml:"rules_enabled,omitempty" json:"rules_enabled"`
}

type Output struct {
	Root      string `yaml:"root,omitempty" json:"root"`
	Collision string `yaml:"collision,omitempty" json:"collision"`
	Layout    string `yaml:"layout,omitempty" json:"layout"`
}

type Uncategorized struct {
	Action    string `yaml:"action,omitempty" json:"action"`
	Directory string `yaml:"directory,omitempty" json:"directory,omitempty"`
}

type Content struct {
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled"`
	AllowPatterns []string `yaml:"allow_patterns,omitempty" json:"allow_patterns"`
	MaxBytes      int64    `yaml:"max_bytes,omitempty" json:"max_bytes"`
	Authorized    bool     `yaml:"-" json:"authorized"`
}

type Jev struct {
	Endpoint         string           `yaml:"endpoint,omitempty" json:"endpoint"`
	Model            string           `yaml:"model,omitempty" json:"model"`
	Threshold        float64          `yaml:"threshold,omitempty" json:"threshold"`
	TimeoutSeconds   int              `yaml:"timeout_seconds,omitempty" json:"timeout_seconds"`
	MaxRetries       int              `yaml:"max_retries,omitempty" json:"max_retries"`
	Concurrency      int              `yaml:"concurrency,omitempty" json:"concurrency"`
	BatchSize        int              `yaml:"batch_size,omitempty" json:"batch_size"`
	FolderEvaluation FolderEvaluation `yaml:"folder_evaluation,omitempty" json:"folder_evaluation"`
	Content          Content          `yaml:"content,omitempty" json:"content"`
}

type FolderEvaluation struct {
	Enabled    *bool `yaml:"enabled,omitempty" json:"enabled"`
	MaxEntries int   `yaml:"max_entries,omitempty" json:"max_entries"`
}

type History struct {
	Enabled    *bool  `yaml:"enabled,omitempty" json:"enabled"`
	Directory  string `yaml:"directory,omitempty" json:"directory,omitempty"`
	MaxEntries int    `yaml:"max_entries,omitempty" json:"max_entries"`
}

type Category struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name,omitempty" json:"name"`
	Enabled     *bool    `yaml:"enabled,omitempty" json:"enabled"`
	Description string   `yaml:"description,omitempty" json:"description"`
	Directory   string   `yaml:"directory,omitempty" json:"directory"`
	Threshold   *float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"`
}

type Rule struct {
	ID       string   `yaml:"id" json:"id"`
	Enabled  *bool    `yaml:"enabled,omitempty" json:"enabled"`
	Kinds    []string `yaml:"kinds" json:"kinds"`
	Match    Match    `yaml:"match" json:"match"`
	Category string   `yaml:"category" json:"category"`
	Preset   bool     `yaml:"-" json:"preset,omitempty"`
}

type Match struct {
	Patterns        []string `yaml:"patterns,omitempty" json:"patterns,omitempty"`
	Extensions      []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Depth           *Depth   `yaml:"depth,omitempty" json:"depth,omitempty"`
	ChildExtensions []string `yaml:"child_extensions,omitempty" json:"child_extensions,omitempty"`
}

type Depth struct {
	Min *int `yaml:"min,omitempty" json:"min,omitempty"`
	Max *int `yaml:"max,omitempty" json:"max,omitempty"`
}

func Bool(value bool) *bool { return &value }
func Int(value int) *int    { return &value }

func Enabled(value *bool) bool { return value == nil || *value }

func (c Config) Category(id string) (Category, bool) {
	for _, category := range c.Categories {
		if category.ID == id {
			return category, true
		}
	}
	return Category{}, false
}
