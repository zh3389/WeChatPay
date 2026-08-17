package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/tencentyun/scf-go-lib/cloudfunction"
	"github.com/tencentyun/scf-go-lib/events"
)

// Config 微信支付配置（全部从环境变量读取，不硬编码任何密钥）
type Config struct {
	MchID        string
	AppID        string
	APIv3Key     string
	CertSerialNo string
	NotifyURL    string
	PubKeyID     string
	PrivateKey   *rsa.PrivateKey
	PublicKey    *rsa.PublicKey
}

// APIResponse 统一 JSON 响应
type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

var (
	cfg     *Config
	initErr string // 初始化错误信息（非空表示初始化失败）
)

func main() {
	// 延迟初始化：配置/密钥出错时不崩溃，而是在请求时返回错误 JSON
	// 这样 SCF 不会因进程退出反复重启容器导致 InitContainerTimeout
	c, err := loadConfig()
	if err != nil {
		initErr = err.Error()
		log.Printf("[init] 配置加载失败: %v", err)
	} else {
		cfg = c
		if err := initWeChatPay(); err != nil {
			initErr = err.Error()
			log.Printf("[init] 微信支付客户端初始化失败: %v", err)
		}
	}
	cloudfunction.Start(handler)
}

// handler SCF API Gateway 入口，按 path + method 路由
func handler(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	// 初始化失败时，health 接口返回详细错误帮助排查，其他接口返回 500
	if initErr != "" {
		if strings.TrimRight(event.Path, "/") == "/pay/health" {
			return errResp(500, "init failed: "+initErr), nil
		}
		return errResp(500, "服务未就绪，请查看 /pay/health"), nil
	}

	path := strings.TrimRight(event.Path, "/")
	method := strings.ToUpper(event.Method)

	switch {
	case path == "/pay/native" && method == "POST":
		return handleNativePay(ctx, event)
	case path == "/pay/jsapi" && method == "POST":
		return handleJSAPIPay(ctx, event)
	case path == "/pay/refund" && method == "POST":
		return handleRefund(ctx, event)
	case path == "/pay/notify" && method == "POST":
		return handleNotify(ctx, event)
	case path == "/pay/query" && method == "GET":
		return handleQuery(ctx, event)
	case path == "/pay/health":
		return okResp("ok"), nil
	default:
		return errResp(404, "not found"), nil
	}
}

// ---------- 响应辅助 ----------

func jsonResp(status int, data any) events.APIGatewayResponse {
	body, _ := json.Marshal(data)
	return events.APIGatewayResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}
}

func okResp(data any) events.APIGatewayResponse {
	return jsonResp(200, APIResponse{Code: 0, Message: "ok", Data: data})
}

func errResp(status int, msg string) events.APIGatewayResponse {
	return jsonResp(status, APIResponse{Code: -1, Message: msg})
}

// ---------- 配置加载 ----------

func loadConfig() (*Config, error) {
	c := &Config{
		MchID:        os.Getenv("WXPAY_MCHID"),
		AppID:        os.Getenv("WXPAY_APPID"),
		APIv3Key:     os.Getenv("WXPAY_APIV3KEY"),
		CertSerialNo: os.Getenv("WXPAY_CERT_SERIAL_NO"),
		NotifyURL:    os.Getenv("WXPAY_NOTIFY_URL"),
		PubKeyID:     os.Getenv("WXPAY_PUB_KEY_ID"),
	}

	var err error
	c.PrivateKey, err = loadPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("加载商户私钥失败: %w", err)
	}
	c.PublicKey, err = loadPublicKey()
	if err != nil {
		return nil, fmt.Errorf("加载微信支付公钥失败: %w", err)
	}

	for k, v := range map[string]string{
		"WXPAY_MCHID": c.MchID, "WXPAY_APPID": c.AppID, "WXPAY_APIV3KEY": c.APIv3Key,
		"WXPAY_CERT_SERIAL_NO": c.CertSerialNo, "WXPAY_PUB_KEY_ID": c.PubKeyID,
	} {
		if v == "" {
			return nil, fmt.Errorf("缺少环境变量: %s", k)
		}
	}
	return c, nil
}

// loadPrivateKey 优先从环境变量 WXPAY_PRIVATE_KEY 读取 PEM 内容，其次从 WXPAY_PRIVATE_KEY_PATH 文件路径读取
func loadPrivateKey() (*rsa.PrivateKey, error) {
	if pemStr := os.Getenv("WXPAY_PRIVATE_KEY"); pemStr != "" {
		return parsePrivateKey([]byte(pemStr))
	}
	path := os.Getenv("WXPAY_PRIVATE_KEY_PATH")
	if path == "" {
		path = "keys/apiclient_key.pem"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件失败: %w", err)
	}
	return parsePrivateKey(data)
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	s := normalizePEM(string(data))
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("私钥 PEM 解码失败，内容长度=%d，前40字符=%q", len(s), truncate(s, 40))
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("无法解析 RSA 私钥")
}

// loadPublicKey 优先从环境变量 WXPAY_PUBLIC_KEY 读取 PEM 内容，其次从 WXPAY_PUBLIC_KEY_PATH 文件路径读取
func loadPublicKey() (*rsa.PublicKey, error) {
	if pemStr := os.Getenv("WXPAY_PUBLIC_KEY"); pemStr != "" {
		return parsePublicKey([]byte(pemStr))
	}
	path := os.Getenv("WXPAY_PUBLIC_KEY_PATH")
	if path == "" {
		path = "keys/pub_key.pem"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取公钥文件失败: %w", err)
	}
	return parsePublicKey(data)
}

func parsePublicKey(data []byte) (*rsa.PublicKey, error) {
	s := normalizePEM(string(data))
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("公钥 PEM 解码失败，内容长度=%d，前40字符=%q", len(s), truncate(s, 40))
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("非 RSA 公钥")
	}
	return rsaPub, nil
}

// ---------- 工具函数 ----------

// normalizePEM 归一化 PEM 文本，处理环境变量中各种换行符丢失/转义的情况
func normalizePEM(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)         // 去除可能的外层引号
	s = strings.ReplaceAll(s, "\\n", "\n") // 字面 \n → 真实换行（环境变量转义）
	s = strings.ReplaceAll(s, "\\r", "")   // 去掉 \r 转义
	s = strings.ReplaceAll(s, "\r\n", "\n") // Windows 换行
	s = strings.ReplaceAll(s, "\r", "\n")
	// 在 BEGIN 标记后插入换行（处理内容挤在一行的情况）
	for _, marker := range []string{"-----BEGIN PRIVATE KEY-----", "-----BEGIN PUBLIC KEY-----", "-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN CERTIFICATE-----"} {
		s = strings.ReplaceAll(s, marker, marker+"\n")
	}
	// 在 END 标记前插入换行（处理 base64 和 END 标记挤在一行的情况）
	for _, marker := range []string{"-----END PRIVATE KEY-----", "-----END PUBLIC KEY-----", "-----END RSA PRIVATE KEY-----", "-----END CERTIFICATE-----"} {
		s = strings.ReplaceAll(s, marker, "\n"+marker)
	}
	// 清理多余换行
	for strings.Contains(s, "\n\n") {
		s = strings.ReplaceAll(s, "\n\n", "\n")
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// genOrderNo 生成商户订单号 / 退款单号
func genOrderNo() string {
	return fmt.Sprintf("SCF%s%04d", time.Now().Format("20060102150405"), rand.Intn(10000))
}

// getQueryParam 从 API Gateway 查询参数中取第一个值
func getQueryParam(qs events.APIGatewayQueryString, key string) string {
	if vals, ok := qs[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}
