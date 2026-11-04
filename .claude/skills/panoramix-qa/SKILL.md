---
name: panoramix-qa
description: Panoramix 验收与测试技能包。包含验收方法、headless 浏览器验证脚本、核心回归路径与易漏边界。qa 在验收时使用。
---

# Panoramix 验收技能包

> 每次验收后追加：新的验证手法、易漏边界、历史缺陷模式。

## 一、必跑的门禁

```bash
cd /mnt/d/develop_project/vista
go vet ./...
go test ./internal/... -count=1
GOOS=windows GOARCH=amd64 go build -o /tmp/check.exe . && rm -f /tmp/check.exe
cd frontend && npx vue-tsc --noEmit
```

任一失败即视为阻塞缺陷。

## 二、前端验证（无需 Wails 窗口）

预览页支持 mock 数据，可独立渲染：

```bash
# 启动（先于截图）
cd frontend && npx vite --port 5241 --strictPort

# 截图 / 抓 DOM（Windows Chrome headless）
"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe" \
  --headless=new --disable-gpu --no-sandbox --incognito \
  --virtual-time-budget=25000 --window-size=1600,900 \
  --dump-dom "http://127.0.0.1:5241/preview.html?app=1&view=db&autodata=1" > /tmp/qa.html
```

可用参数：
- `?app=1` 渲染完整 App；`&view=db` 直接进数据库视图
- `&autodata=1` 自动展开第一个库并打开第一张表的数据标签
- `?panel=files|db` 单独渲染组件

**注意**：截图经 PNG 压缩后深色界面偏暗，判断布局用 DOM 测量更可靠：

```javascript
// 在页面注入后 dump-dom 读取
const tb = document.querySelector('.toolbar')
`tb=${tb.clientWidth}/${tb.scrollWidth}`
```

## 三、核心回归路径（每次发版前）

1. **SSH**：连接 → 终端输入 `ls` → vim 打开文件 → 断开重连 → 标签置灰
2. **SFTP**：双击表目录进入 → 上传小文件 → 下载校验内容 → 取消传输后重试（续传）
3. **监控**：打开监控面板 → 指标有数值 → 曲线绘制 → 断开后显示"已停止"
4. **数据库**：连接 → 库/表树 → 单击表（表+数据标签同时出现）→ 双击表（激活数据）→ 查询标签执行 SQL
5. **表结构编辑**：加字段 → SQL 预览 → 确认 → 字段出现 → 删除
6. **数据编辑**：改单元格 → ✓ 应用 → 数据刷新；再改 → ✕ 放弃

## 四、易漏边界

| 场景 | 检查点 |
| --- | --- |
| 无主键表 | 点击单元格应提示无法编辑，而不是静默失败 |
| 空结果集 | 显示"0 行"提示，而不是空白 |
| 表名/列名含反引号或引号 | SQL 转义正确（标识符用 `` ` `` 双写，字符串用 `''`） |
| 大表（>10万行） | 分页不卡死；不自动 COUNT |
| 重复点击 | 同一张表重复单击不重复请求 |
| 危险 DDL | 必须先出现 SQL 预览弹窗 |
| 凭据 | 日志与界面不出现密码；keychain 里能查到条目 |

## 五、历史缺陷模式（重点回归）

1. **布局类**：flex 挤压导致按钮不可见；类名冲突导致样式覆盖；表格内输入框撑宽列
2. **响应式类**：ref 中的普通对象修改不更新界面；import 的 ref 在模板不解包
3. **状态竞态**：加载标志卡死（loading 未复位）；重复请求
4. **数据安全**：UNIQUE 冲突导致更新失败（应用修改时要提示）
