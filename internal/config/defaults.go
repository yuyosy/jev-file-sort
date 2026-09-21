package config

func Defaults() Config {
	return Config{
		Version: 1,
		Mode:    "simple",
		Scan: Scan{
			Recursive:      Bool(false),
			IncludeHidden:  Bool(false),
			FollowSymlinks: Bool(false),
		},
		Output:        Output{Root: "Sorted", Collision: "skip"},
		Uncategorized: Uncategorized{Action: "leave", Directory: "Unsorted"},
		Content:       Content{Enabled: Bool(false), MaxBytes: 32768},
		Jev: Jev{
			Endpoint:       "https://api.typesafe.ai/v1/systemone",
			Model:          "jev-latest",
			Threshold:      0.80,
			TimeoutSeconds: 30,
			MaxRetries:     3,
			Concurrency:    4,
			FolderEvaluation: FolderEvaluation{
				Enabled:    Bool(false),
				MaxEntries: 100,
			},
		},
		History: History{Enabled: Bool(true), MaxEntries: 100},
		Categories: []Category{
			{ID: "documents", Name: "Documents", Enabled: Bool(true), Description: "Office documents and portable documents", Directory: "Documents"},
			{ID: "text", Name: "Text", Enabled: Bool(true), Description: "Plain text, markup, and source-like text", Directory: "Text"},
			{ID: "images", Name: "Images", Enabled: Bool(true), Description: "Raster and vector images", Directory: "Images"},
			{ID: "audio", Name: "Audio", Enabled: Bool(true), Description: "Audio files", Directory: "Audio"},
			{ID: "video", Name: "Video", Enabled: Bool(true), Description: "Video files", Directory: "Video"},
			{ID: "archives", Name: "Archives", Enabled: Bool(true), Description: "Compressed files and archives", Directory: "Archives"},
		},
		Rules: []Rule{
			{ID: "preset-documents", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp"}}, Category: "documents", Preset: true},
			{ID: "preset-text", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".txt", ".md", ".rst", ".csv", ".tsv", ".json", ".yaml", ".yml", ".xml", ".toml", ".ini", ".log"}}, Category: "text", Preset: true},
			{ID: "preset-images", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff", ".svg", ".heic"}}, Category: "images", Preset: true},
			{ID: "preset-audio", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".mp3", ".wav", ".flac", ".aac", ".m4a", ".ogg", ".opus"}}, Category: "audio", Preset: true},
			{ID: "preset-video", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".mp4", ".mkv", ".mov", ".avi", ".webm", ".m4v"}}, Category: "video", Preset: true},
			{ID: "preset-archives", Enabled: Bool(true), Kinds: []string{"file"}, Match: Match{Extensions: []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".7z", ".rar", ".gz", ".bz2", ".xz"}}, Category: "archives", Preset: true},
		},
	}
}
