#!/bin/bash
# 微信支付完整测试脚本
# 用法: ./test.sh [native|jsapi|query|refund|health]

BASE="https://<function-url>.ap-chengdu.tencentscf.com"
CMD=${1:-native}

echo "=========================================="
echo " 微信支付测试  ($CMD)"
echo "=========================================="

case "$CMD" in
  health)
    echo "→ 健康检查"
    curl -s "$BASE/pay/health" | python3 -m json.tool
    ;;

  native)
    echo "→ Native 下单（1分钱）"
    RESP=$(curl -s -X POST "$BASE/pay/native" \
      -H "Content-Type: application/json" \
      -d '{"description":"测试商品-1分钱","amount":1}')
    echo "$RESP" | python3 -m json.tool

    CODE_URL=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['code_url'])" 2>/dev/null)
    TRADE_NO=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['out_trade_no'])" 2>/dev/null)

    if [ -n "$CODE_URL" ]; then
      echo ""
      echo "✅ 下单成功！"
      echo "订单号: $TRADE_NO"
      echo ""
      echo "请用微信扫码支付（复制以下链接到浏览器会显示二维码）："
      echo "  https://api.qrserver.com/v1/create-qr-code/?size=300x300&data=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$CODE_URL'))")"
      echo ""
      echo "或安装 qrencode 后执行: qrencode -t ANSI '$CODE_URL'"
      echo ""
      echo "支付完成后查询订单:"
      echo "  ./test.sh query $TRADE_NO"
    fi
    ;;

  query)
    TRADE_NO=${2:-""}
    if [ -z "$TRADE_NO" ]; then
      echo "用法: ./test.sh query <out_trade_no>"
      exit 1
    fi
    echo "→ 查询订单: $TRADE_NO"
    curl -s "$BASE/pay/query?out_trade_no=$TRADE_NO" | python3 -m json.tool
    ;;

  refund)
    TRADE_NO=${2:-""}
    if [ -z "$TRADE_NO" ]; then
      echo "用法: ./test.sh refund <out_trade_no> [total]"
      echo "示例: ./test.sh refund SCF202608141015074888 1"
      exit 1
    fi
    TOTAL=${3:-1}
    echo "→ 退款: 订单=$TRADE_NO 退款金额=${TOTAL}分"
    curl -s -X POST "$BASE/pay/refund" \
      -H "Content-Type: application/json" \
      -d "{\"out_trade_no\":\"$TRADE_NO\",\"refund\":$TOTAL,\"total\":$TOTAL,\"reason\":\"测试退款\"}" | python3 -m json.tool
    ;;

  jsapi)
    OPENID=${2:-"oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"}
    echo "→ JSAPI 下单（1分钱，openid=$OPENID）"
    curl -s -X POST "$BASE/pay/jsapi" \
      -H "Content-Type: application/json" \
      -d "{\"description\":\"测试商品-1分钱\",\"amount\":1,\"openid\":\"$OPENID\"}" | python3 -m json.tool
    ;;

  *)
    echo "用法: ./test.sh [health|native|query|refund|jsapi]"
    echo ""
    echo "  ./test.sh health              健康检查"
    echo "  ./test.sh native              Native下单(扫码)"
    echo "  ./test.sh query <订单号>       查询订单"
    echo "  ./test.sh refund <订单号> [金额] 退款"
    echo "  ./test.sh jsapi [openid]      JSAPI下单"
    ;;
esac
