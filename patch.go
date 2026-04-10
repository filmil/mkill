func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "mkill", "config.json"), nil
}

func loadConfig() map[string]bool {
	defaultProtected := map[string]bool{
		"chrome-remote-desktop":      true,
		"chrome-remote-desktop-host": true,
		"chrome":                     true,
	}
	path, err := getConfigPath()
	if err != nil {
		return defaultProtected
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultProtected
	}
	var cfg struct {
		Protected []string `json:"protected"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultProtected
	}
	protected := make(map[string]bool)
	for _, p := range cfg.Protected {
		protected[p] = true
	}
	return protected
}

func saveConfig(protected map[string]bool) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var cfg struct {
		Protected []string `json:"protected"`
	}
	for p := range protected {
		cfg.Protected = append(cfg.Protected, p)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
