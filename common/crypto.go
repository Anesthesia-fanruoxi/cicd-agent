package common

import (
	"bytes"
	"cicd-agent/config"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// DecryptAndDecompress 解密并解压数据
func DecryptAndDecompress(data string) ([]byte, error) {
	// 使用配置中的salt作为密钥
	encryptionSalt := config.GetEncryptionSalt()
	key := []byte(encryptionSalt)

	// 1. Base64解码
	encryptedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("Base64解码失败: %v", err))
		return nil, fmt.Errorf("base64解码失败: %v", err)
	}
	// AppLogger.Info(fmt.Sprintf("Base64解码后长度: %d", len(encryptedData)))

	// 2. AES-GCM解密
	if len(encryptedData) < 12 {
		AppLogger.Error(fmt.Sprintf("加密数据长度不足: %d", len(encryptedData)))
		return nil, fmt.Errorf("加密数据长度不足")
	}
	nonce := encryptedData[:12]
	ciphertext := encryptedData[12:]
	// AppLogger.Info(fmt.Sprintf("Nonce长度: %d, 密文长度: %d", len(nonce), len(ciphertext)))

	block, err := aes.NewCipher(key)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("创建AES cipher失败: %v", err))
		return nil, fmt.Errorf("创建AES cipher失败: %v", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("创建GCM失败: %v", err))
		return nil, fmt.Errorf("创建GCM失败: %v", err)
	}

	compressedData, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("AES-GCM解密失败: %v", err))
		return nil, fmt.Errorf("AES-GCM解密失败: %v", err)
	}
	// AppLogger.Info(fmt.Sprintf("解密后的压缩数据长度: %d", len(compressedData)))

	// 3. gzip解压缩
	reader := bytes.NewReader(compressedData)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("创建gzip reader失败: %v", err))
		return nil, fmt.Errorf("创建gzip reader失败: %v", err)
	}
	defer func(gzipReader *gzip.Reader) {
		err := gzipReader.Close()
		if err != nil {

		}
	}(gzipReader)

	result, err := io.ReadAll(gzipReader)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("读取解压数据失败: %v", err))
		return nil, fmt.Errorf("读取解压数据失败: %v", err)
	}

	// AppLogger.Info(fmt.Sprintf("解压后的数据长度: %d", len(result)))
	return result, nil
}

// CompressAndEncrypt 压缩并加密数据
func CompressAndEncrypt(data []byte) (string, error) {
	// 压缩数据
	var compressedBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedBuf)

	_, err := gzipWriter.Write(data)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("压缩数据失败: %v", err))
		return "", fmt.Errorf("压缩数据失败: %v", err)
	}

	// 关闭gzip写入器以确保所有数据都被写入
	if err := gzipWriter.Close(); err != nil {
		AppLogger.Error(fmt.Sprintf("关闭gzip写入器失败: %v", err))
		return "", fmt.Errorf("关闭gzip写入器失败: %v", err)
	}

	compressedData := compressedBuf.Bytes()

	// 获取加密盐值
	encryptionSalt := config.GetEncryptionSalt()

	// 创建AES加密器
	block, err := aes.NewCipher([]byte(encryptionSalt))
	if err != nil {
		AppLogger.Error(fmt.Sprintf("创建AES加密器失败: %v", err))
		return "", fmt.Errorf("创建AES加密器失败: %v", err)
	}

	// 创建GCM模式加密器
	aesGcm, err := cipher.NewGCM(block)
	if err != nil {
		AppLogger.Error(fmt.Sprintf("创建GCM失败: %v", err))
		return "", fmt.Errorf("创建GCM失败: %v", err)
	}

	// 创建12字节的nonce
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		AppLogger.Error(fmt.Sprintf("生成nonce失败: %v", err))
		return "", fmt.Errorf("生成nonce失败: %v", err)
	}

	// 加密数据
	ciphertext := aesGcm.Seal(nil, nonce, compressedData, nil)

	// 将nonce和密文组合
	result := append(nonce, ciphertext...)

	// 将结果转换为base64编码
	base64Result := base64.StdEncoding.EncodeToString(result)

	return base64Result, nil
}
