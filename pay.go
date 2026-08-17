package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/tencentyun/scf-go-lib/events"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

// 微信支付 SDK 全局实例（SCF 热启动时复用）
var (
	wxClient  *core.Client
	notifyHdl *notify.Handler
	nativeSvc native.NativeApiService
	jsapiSvc  jsapi.JsapiApiService
	refundSvc refunddomestic.RefundsApiService
)

// initWeChatPay 使用「微信支付公钥模式」初始化客户端（无需下载平台证书，更安全更极简）
func initWeChatPay() error {
	client, err := core.NewClient(context.Background(),
		option.WithWechatPayPublicKeyAuthCipher(
			cfg.MchID,
			cfg.CertSerialNo,
			cfg.PrivateKey,
			cfg.PubKeyID,
			cfg.PublicKey,
		),
	)
	if err != nil {
		return fmt.Errorf("初始化微信支付客户端失败: %w", err)
	}
	wxClient = client
	nativeSvc = native.NativeApiService{Client: client}
	jsapiSvc = jsapi.JsapiApiService{Client: client}
	refundSvc = refunddomestic.RefundsApiService{Client: client}

	// 回调验签 handler：用微信支付公钥验签 + APIv3Key 解密
	notifyHdl = notify.NewNotifyHandler(cfg.APIv3Key,
		verifiers.NewSHA256WithRSAPubkeyVerifier(cfg.PubKeyID, *cfg.PublicKey))
	return nil
}

// ---------- 请求结构体 ----------

type payReq struct {
	Description string `json:"description"` // 商品描述
	OutTradeNo  string `json:"out_trade_no"` // 商户订单号（可选，为空自动生成）
	Amount      int64  `json:"amount"`       // 金额，单位：分
	Attach      string `json:"attach"`       // 附加数据（可选）
	Openid      string `json:"openid"`       // 用户 openid（JSAPI 必填）
}

type refundReq struct {
	OutTradeNo  string `json:"out_trade_no"`  // 原商户订单号
	OutRefundNo string `json:"out_refund_no"` // 商户退款单号（可选，为空自动生成）
	Reason      string `json:"reason"`        // 退款原因（可选）
	Refund      int64  `json:"refund"`        // 退款金额，单位：分
	Total       int64  `json:"total"`         // 原订单总金额，单位：分
	NotifyURL   string `json:"notify_url"`    // 退款回调地址（可选，默认用全局）
}

// ---------- Native 下单（PC 扫码） ----------

func handleNativePay(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	var req payReq
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
		return errResp(400, "请求参数错误"), nil
	}
	if req.Description == "" || req.Amount <= 0 {
		return errResp(400, "description 和 amount 为必填"), nil
	}
	if req.OutTradeNo == "" {
		req.OutTradeNo = genOrderNo()
	}

	resp, _, err := nativeSvc.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(cfg.AppID),
		Mchid:       core.String(cfg.MchID),
		Description: core.String(req.Description),
		OutTradeNo:  core.String(req.OutTradeNo),
		TimeExpire:  core.Time(time.Now().Add(30 * time.Minute)),
		Attach:      core.String(req.Attach),
		NotifyUrl:   core.String(cfg.NotifyURL),
		Amount: &native.Amount{
			Currency: core.String("CNY"),
			Total:    core.Int64(req.Amount),
		},
	})
	if err != nil {
		log.Printf("Native 下单失败: %v", err)
		return errResp(500, "下单失败: "+err.Error()), nil
	}

	return okResp(map[string]any{
		"out_trade_no": req.OutTradeNo,
		"code_url":     resp.CodeUrl,
	}), nil
}

// ---------- JSAPI 下单（小程序/公众号） ----------

func handleJSAPIPay(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	var req payReq
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
		return errResp(400, "请求参数错误"), nil
	}
	if req.Description == "" || req.Amount <= 0 || req.Openid == "" {
		return errResp(400, "description, amount, openid 为必填"), nil
	}
	if req.OutTradeNo == "" {
		req.OutTradeNo = genOrderNo()
	}

	// PrepayWithRequestPayment 返回含签名的调起支付参数，前端可直接用
	resp, _, err := jsapiSvc.PrepayWithRequestPayment(ctx, jsapi.PrepayRequest{
		Appid:       core.String(cfg.AppID),
		Mchid:       core.String(cfg.MchID),
		Description: core.String(req.Description),
		OutTradeNo:  core.String(req.OutTradeNo),
		TimeExpire:  core.Time(time.Now().Add(30 * time.Minute)),
		Attach:      core.String(req.Attach),
		NotifyUrl:   core.String(cfg.NotifyURL),
		Amount: &jsapi.Amount{
			Currency: core.String("CNY"),
			Total:    core.Int64(req.Amount),
		},
		Payer: &jsapi.Payer{
			Openid: core.String(req.Openid),
		},
	})
	if err != nil {
		log.Printf("JSAPI 下单失败: %v", err)
		return errResp(500, "下单失败: "+err.Error()), nil
	}

	return okResp(map[string]any{
		"out_trade_no": req.OutTradeNo,
		"payment":      resp,
	}), nil
}

// ---------- 退款 ----------

func handleRefund(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	var req refundReq
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
		return errResp(400, "请求参数错误"), nil
	}
	if req.OutTradeNo == "" || req.Refund <= 0 || req.Total <= 0 {
		return errResp(400, "out_trade_no, refund, total 为必填"), nil
	}
	if req.Refund > req.Total {
		return errResp(400, "退款金额不能大于原订单金额"), nil
	}
	if req.OutRefundNo == "" {
		req.OutRefundNo = genOrderNo()
	}

	notifyURL := req.NotifyURL
	if notifyURL == "" {
		notifyURL = cfg.NotifyURL
	}

	resp, _, err := refundSvc.Create(ctx, refunddomestic.CreateRequest{
		OutTradeNo:  core.String(req.OutTradeNo),
		OutRefundNo: core.String(req.OutRefundNo),
		Reason:      core.String(req.Reason),
		NotifyUrl:   core.String(notifyURL),
		Amount: &refunddomestic.AmountReq{
			Currency: core.String("CNY"),
			Refund:   core.Int64(req.Refund),
			Total:    core.Int64(req.Total),
		},
	})
	if err != nil {
		log.Printf("退款失败: %v", err)
		return errResp(500, "退款失败: "+err.Error()), nil
	}

	return okResp(map[string]any{
		"out_refund_no": req.OutRefundNo,
		"refund":        resp,
	}), nil
}

// ---------- 回调通知验签 ----------

func handleNotify(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	// 从 SCF 事件重建 http.Request，供 SDK 读取 Wechatpay-* 签名头并验签
	httpReq, err := http.NewRequestWithContext(ctx, "POST", event.Path, strings.NewReader(event.Body))
	if err != nil {
		log.Printf("构造请求失败: %v", err)
		return notifyResp("FAIL", "内部错误"), nil
	}
	for k, v := range event.Headers {
		httpReq.Header.Set(k, v)
	}

	// SDK 自动验签 + 用 APIv3Key 解密 resource
	transaction := new(payments.Transaction)
	_, err = notifyHdl.ParseNotifyRequest(ctx, httpReq, transaction)
	if err != nil {
		log.Printf("回调验签失败: %v", err)
		return notifyResp("FAIL", "验签失败"), nil
	}

	// ====== 业务逻辑在此处理（更新订单状态、发货等）======
	log.Printf("支付成功: out_trade_no=%s transaction_id=%s amount=%d fen",
		transaction.OutTradeNo, transaction.TransactionId, transaction.Amount.Total)

	return notifyResp("SUCCESS", "成功"), nil
}

// notifyResp 返回微信支付要求的回调响应格式
func notifyResp(code, msg string) events.APIGatewayResponse {
	return jsonResp(200, map[string]string{"code": code, "message": msg})
}

// ---------- 查询订单 ----------

func handleQuery(ctx context.Context, event events.APIGatewayRequest) (events.APIGatewayResponse, error) {
	outTradeNo := getQueryParam(event.QueryString, "out_trade_no")
	if outTradeNo == "" {
		return errResp(400, "out_trade_no 为必填"), nil
	}

	resp, _, err := nativeSvc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(cfg.MchID),
	})
	if err != nil {
		log.Printf("查询订单失败: %v", err)
		return errResp(500, "查询失败: "+err.Error()), nil
	}

	return okResp(resp), nil
}
