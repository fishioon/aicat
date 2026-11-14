package channelconfig

type MessageAgent struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Binary  string `yaml:"binary,omitempty" json:"binary"`
}
