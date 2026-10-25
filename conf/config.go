package conf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

type ConfigType struct {
	Proxy    string      `yaml:"proxy"`
	Username string      `yaml:"username"`
	Password string      `yaml:"password"`
	Aria2    Aria2Config `yaml:"aria2"`
	Open     OpenConfig  `yaml:"open"`
}

type Aria2Config struct {
	RPCUrl         string `yaml:"rpc_url"`
	Secret         string `yaml:"secret"`
	MaxConnections int    `yaml:"max_connections"`
	Split          int    `yaml:"split"`
	MinSplitSize   string `yaml:"min_split_size"`
	DownloadDir    string `yaml:"download_dir"`
}

type OpenConfig struct {
	DownloadDir string   `yaml:"download_dir"`
	Default     []string `yaml:"default"`
	Text        []string `yaml:"text"`
	Image       []string `yaml:"image"`
	Video       []string `yaml:"video"`
	Audio       []string `yaml:"audio"`
	PDF         []string `yaml:"pdf"`
}

var Config ConfigType

func (c *ConfigType) UseProxy() bool {
	return len(c.Proxy) != 0
}

func (c *Aria2Config) GetRPCUrl() string {
	if c.RPCUrl == "" {
		return "http://127.0.0.1:6800/jsonrpc"
	}
	return c.RPCUrl
}

func (c *Aria2Config) GetMaxConnections() int {
	if c.MaxConnections <= 0 {
		return 16
	}
	return c.MaxConnections
}

func (c *Aria2Config) GetSplit() int {
	if c.Split <= 0 {
		return 16
	}
	return c.Split
}

func (c *Aria2Config) GetMinSplitSize() string {
	if c.MinSplitSize == "" {
		return "1M"
	}
	return c.MinSplitSize
}

func (c *Aria2Config) GetDownloadDir() string {
	if c.DownloadDir == "" {
		return "."
	}
	return c.DownloadDir
}

func InitConfig(path string) error {
	if readFromBinary() == nil {
		return nil
	}

	_, err := os.Stat(path)
	switch os.IsNotExist(err) {
	case true:
		if err := readFromConfigDir(); err != nil {
			return err
		}
	case false:
		if err := readFromPath(path); err != nil {
			return err
		}
	}

	if len(Config.Proxy) != 0 && !strings.Contains(Config.Proxy, "://") {
		return fmt.Errorf("proxy should contains ://")
	}
	return nil
}

func readFromBinary() error {
	f, err := os.Open(os.Args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}

	var end_magic = make([]byte, 10)
	n, err := f.ReadAt(end_magic, stat.Size()-10)
	if err != nil {
		return err
	}

	if n != 10 {
		return fmt.Errorf("read end_magic err: %d", n)
	}

	if !bytes.Equal(end_magic, []byte("config.yml")) {
		return fmt.Errorf("not a pikpakidm binary")
	}

	var size = make([]byte, 4)
	n, err = f.ReadAt(size, stat.Size()-14)

	if err != nil {
		return err
	}

	if n != 4 {
		return fmt.Errorf("read size err: %d", n)
	}

	configSize := int64(binary.LittleEndian.Uint32(size))
	configBuf := make([]byte, configSize)

	n, err = f.ReadAt(configBuf, stat.Size()-14-configSize)

	if err != nil || n != int(configSize) {
		return err
	}

	if n != int(configSize) {
		return fmt.Errorf("read config size err: %d", n)
	}

	return yaml.Unmarshal(configBuf, &Config)
}

func readFromPath(path string) error {
	return readConfig(path)
}

func readFromConfigDir() error {
	path, err := DefaultConfigPath()
	if err != nil {
		return err
	}
	return readConfig(path)
}

func DefaultConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "pikpakidm", "config.yml"), nil
}

func readConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bs, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(bs, &Config)
	if err != nil {
		return err
	}
	return nil
}
