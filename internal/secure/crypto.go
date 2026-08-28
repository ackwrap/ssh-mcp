package secure

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	fernet "github.com/fernet/fernet-go"
	"golang.org/x/crypto/pbkdf2"
)

const (
	PBKDF2Iterations = 300_000
	KeyLength        = 32
)

func AppDataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		var err error
		base, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户目录失败: %w", err)
		}
	}
	dir := filepath.Join(base, "ssh_mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	return dir, nil
}

func ConfigPath() (string, error)        { return dataPath("config.json") }
func KeyFilePath() (string, error)       { return dataPath("key.bin") }
func SessionSecretPath() (string, error) { return dataPath("session.secret") }

func dataPath(name string) (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func NewSalt() ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("生成随机盐失败: %w", err)
	}
	return salt, nil
}

func DeriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, PBKDF2Iterations, KeyLength, sha256.New)
}

func MakeMasterHash(password string, salt []byte) string {
	verifySalt := append(append([]byte(nil), salt...), []byte("verify")...)
	digest := pbkdf2.Key([]byte(password), verifySalt, PBKDF2Iterations, KeyLength, sha256.New)
	return hex.EncodeToString(digest)
}

func VerifyMaster(password string, salt []byte, expectedHex string) bool {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	actual, err := hex.DecodeString(MakeMasterHash(password, salt))
	return err == nil && hmac.Equal(actual, expected)
}

func Encrypt(key, plaintext []byte) (string, error) {
	if len(key) != KeyLength {
		return "", errors.New("加密密钥长度无效")
	}
	var fernetKey fernet.Key
	copy(fernetKey[:], key)
	token, err := fernet.EncryptAndSign(plaintext, &fernetKey)
	if err != nil {
		return "", fmt.Errorf("加密失败: %w", err)
	}
	return string(token), nil
}

func Decrypt(key []byte, token string) ([]byte, error) {
	if len(key) != KeyLength {
		return nil, errors.New("解密密钥长度无效")
	}
	var fernetKey fernet.Key
	copy(fernetKey[:], key)
	plaintext := fernet.VerifyAndDecrypt([]byte(token), 0, []*fernet.Key{&fernetKey})
	if plaintext == nil {
		return nil, errors.New("解密失败：主密码错误或数据已损坏")
	}
	return plaintext, nil
}

func WriteKeyFile(key []byte) error {
	path, err := KeyFilePath()
	if err != nil {
		return err
	}
	return atomicSecretWrite(path, key)
}

func ReadKeyFile() ([]byte, error) {
	path, err := KeyFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取解锁密钥失败: %w", err)
	}
	return data, nil
}

func DeleteKeyFile() error {
	path, err := KeyFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func ResolveKey(saltHex string) ([]byte, error) {
	key, err := ReadKeyFile()
	if err == nil && len(key) > 0 {
		return key, nil
	}
	if password := os.Getenv("SSH_MCP_PASSWORD"); password != "" && saltHex != "" {
		salt, decodeErr := hex.DecodeString(saltHex)
		if decodeErr != nil {
			return nil, fmt.Errorf("配置 salt 无效: %w", decodeErr)
		}
		return DeriveKey(password, salt), nil
	}
	return nil, errors.New("无法解密凭据：请先启动 SSH-MCP 托盘程序并在 WebUI 中解锁（登录一次即可），或在启动 MCP 服务器时设置 SSH_MCP_PASSWORD 环境变量")
}

func LoadOrCreateSessionSecret() ([]byte, error) {
	path, err := SessionSecretPath()
	if err != nil {
		return nil, err
	}
	data, readErr := os.ReadFile(path)
	if readErr == nil && len(data) == 32 {
		return data, nil
	}
	return RotateSessionSecret()
}

func RotateSessionSecret() ([]byte, error) {
	path, err := SessionSecretPath()
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("生成会话密钥失败: %w", err)
	}
	if err := atomicSecretWrite(path, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func atomicSecretWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	restrictPermissions(tmp)
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("写入密钥文件失败: %w", err)
	}
	return nil
}

func restrictPermissions(path string) {
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
		return
	}
	user := os.Getenv("USERNAME")
	if user == "" {
		return
	}
	_ = exec.Command("icacls", path, "/inheritance:r", "/grant:r", user+":F").Run()
}
