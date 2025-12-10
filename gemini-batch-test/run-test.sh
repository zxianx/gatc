#!/bin/bash

# Gemini Batch API 便捷测试脚本
# 使用方法: ./run-test.sh [token-file] [test-type] [count]

set -e

# 默认参数
DEFAULT_TOKEN_FILE="config/tokens/my-token.json"
DEFAULT_TEST_TYPE="simple"
DEFAULT_COUNT="10"

# 解析命令行参数
TOKEN_FILE="${1:-$DEFAULT_TOKEN_FILE}"
TEST_TYPE="${2:-$DEFAULT_TEST_TYPE}"
COUNT="${3:-$DEFAULT_COUNT}"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印彩色消息
print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# 显示帮助信息
show_help() {
    echo "Gemini Batch API 测试脚本"
    echo ""
    echo "使用方法:"
    echo "  ./run-test.sh [token-file] [test-type] [count]"
    echo ""
    echo "参数说明:"
    echo "  token-file  Token配置文件路径 (默认: config/tokens/my-token.json)"
    echo "  test-type   测试类型 (simple/complex/mixed/advanced，默认: simple)"  
    echo "  count       请求数量 (默认: 10)"
    echo ""
    echo "示例:"
    echo "  ./run-test.sh                                              # 使用默认配置"
    echo "  ./run-test.sh config/tokens/my-token.json simple 20       # 20个简单请求"
    echo "  ./run-test.sh config/tokens/my-token.json mixed 50        # 50个混合请求"
    echo "  ./run-test.sh config/tokens/my-token.json complex 100     # 100个复杂请求"
    echo "  ./run-test.sh config/tokens/my-token.json advanced 30     # 30个高级配置请求"
    echo ""
    echo "环境要求:"
    echo "  - Go 1.21+"
    echo "  - 有效的Gemini API Key"
    echo "  - 稳定的网络连接"
}

# 检查参数
if [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
    show_help
    exit 0
fi

# 检查Go是否安装
if ! command -v go &> /dev/null; then
    print_error "Go未安装，请先安装Go 1.21+"
    exit 1
fi

# 检查Token文件是否存在
if [[ ! -f "$TOKEN_FILE" ]]; then
    print_error "Token配置文件不存在: $TOKEN_FILE"
    print_info "请先配置Token文件:"
    print_info "  cp config/tokens/example.json $TOKEN_FILE"
    print_info "  vim $TOKEN_FILE  # 填入真实的API Key"
    exit 1
fi

# 验证测试类型
if [[ "$TEST_TYPE" != "simple" ]] && [[ "$TEST_TYPE" != "complex" ]] && [[ "$TEST_TYPE" != "mixed" ]] && [[ "$TEST_TYPE" != "advanced" ]]; then
    print_error "无效的测试类型: $TEST_TYPE"
    print_info "支持的类型: simple, complex, mixed, advanced"
    exit 1
fi

# 验证请求数量
if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [ "$COUNT" -lt 1 ] || [ "$COUNT" -gt 500 ]; then
    print_error "无效的请求数量: $COUNT"
    print_info "支持的范围: 1-500"
    exit 1
fi

# 显示测试配置
echo "🚀 Gemini Batch API 测试启动"
echo "================================"
print_info "Token文件: $TOKEN_FILE"
print_info "测试类型: $TEST_TYPE"
print_info "请求数量: $COUNT"
echo ""

# 预估测试时间
estimate_time() {
    local type=$1
    local count=$2
    
    if [[ "$type" == "simple" ]]; then
        if [ "$count" -le 20 ]; then
            echo "5-15分钟"
        else
            echo "15-30分钟"
        fi
    elif [[ "$type" == "complex" ]]; then
        if [ "$count" -le 20 ]; then
            echo "30-60分钟"
        elif [ "$count" -le 50 ]; then
            echo "1-3小时"
        else
            echo "3-8小时"
        fi
    elif [[ "$type" == "mixed" ]]; then
        if [ "$count" -le 20 ]; then
            echo "10-30分钟"
        elif [ "$count" -le 50 ]; then
            echo "30-90分钟"
        else
            echo "1-4小时"
        fi
    else  # advanced
        if [ "$count" -le 10 ]; then
            echo "20-45分钟"
        elif [ "$count" -le 30 ]; then
            echo "1-2小时"
        else
            echo "2-6小时"
        fi
    fi
}

estimated_time=$(estimate_time "$TEST_TYPE" "$COUNT")
print_warning "预估测试时间: $estimated_time"
print_warning "测试过程中请保持网络连接稳定"
echo ""

# 询问用户确认
read -p "是否继续执行测试? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    print_info "测试已取消"
    exit 0
fi

echo ""

# 检查依赖
print_info "检查Go模块依赖..."
if ! go mod download; then
    print_error "下载依赖失败"
    exit 1
fi
print_success "依赖检查完成"

# 执行测试
print_info "开始执行完整测试..."
echo ""

START_TIME=$(date +%s)

# 执行完整测试
if go run cmd/full-test/main.go \
    -token-file="$TOKEN_FILE" \
    -type="$TEST_TYPE" \
    -count="$COUNT" \
    -interval=10 \
    -save-all=true; then
    
    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))
    DURATION_MIN=$((DURATION / 60))
    DURATION_SEC=$((DURATION % 60))
    
    echo ""
    print_success "测试完成!"
    print_info "实际耗时: ${DURATION_MIN}分${DURATION_SEC}秒"
    
    # 显示输出目录
    echo ""
    print_info "测试结果已保存到:"
    print_info "  - 测试数据: output/test-data/"
    print_info "  - 测试报告: output/reports/"
    print_info "  - 处理结果: output/results/"
    
else
    print_error "测试失败"
    exit 1
fi

echo ""
print_success "Gemini Batch API 测试完成!"

# 显示后续建议
echo ""
print_info "💡 后续建议:"
if [[ "$COUNT" -le 20 ]] && [[ "$TEST_TYPE" == "simple" ]]; then
    print_info "  - 可以尝试更大规模的测试: ./run-test.sh $TOKEN_FILE mixed 50"
elif [[ "$COUNT" -le 50 ]]; then
    print_info "  - 可以尝试复杂请求测试: ./run-test.sh $TOKEN_FILE complex 100"
fi
print_info "  - 查看详细报告: cat output/reports/*-report.json | jq"
print_info "  - 对比不同配置的测试结果，优化批处理策略"