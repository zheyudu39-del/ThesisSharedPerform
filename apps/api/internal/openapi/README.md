# 开放 API

只读的公开数据接口，供第三方程序（文献聚合、个人站点嵌入、数据分析）使用。

## 认证

所有 `/openapi/v1/*` 端点都要求 `X-API-Key` 请求头：

```bash
curl -H "X-API-Key: tsk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  http://localhost:8080/openapi/v1/papers?limit=10
```

密钥在网站「设置 → API Key」中创建。**明文只在创建响应里出现一次**，
服务端只保存 SHA-256 哈希，丢失只能撤销后重建。

### 权限范围（scopes）

| scope | 允许访问 |
|---|---|
| `read:papers` | `/papers`、`/papers/{id}` |
| `read:posts` | `/posts` |
| `read:users` | `/users/{handle}` |

缺少对应 scope 时返回 `403 insufficient_scope`，而不是静默降级——
让调用方明确知道要补哪个授权。

### 限流

每把密钥每分钟 60 次。超限返回 `429`，并带 `Retry-After: 60`。
按密钥而非按 IP 计数：第三方程序常部署在共享出口 IP 的云上，
按 IP 会让同机房的多个使用者互相挤占配额。

## 端点

### `GET /openapi/v1/papers`

```
?limit=20   # 1-100，默认 20
?offset=0   # 非负，默认 0
```

```json
{
  "papers": [
    {
      "id": "0193...",
      "doi": "10.1145/3292500",
      "title": "Attention Is All You Need",
      "authors": ["Ashish Vaswani"],
      "abstract": "...",
      "venue": "NeurIPS",
      "publishedYear": 2017,
      "citationCount": 100000,
      "url": "https://doi.org/10.1145/3292500"
    }
  ],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

### `GET /openapi/v1/papers/{id}`

返回 `{"paper": {...}}`，结构同上。

### `GET /openapi/v1/posts`

```
?limit=20   # 1-100，默认 20
```

```json
{
  "posts": [
    {
      "id": "0193...",
      "type": "paper_share",
      "author": "张三",
      "authorHandle": "zhangsan",
      "contentText": "这篇论文的贡献在于...",
      "likeCount": 12,
      "commentCount": 3,
      "paperId": "0193...",
      "createdAt": "2026-11-21T08:00:00Z",
      "url": "/posts/0193..."
    }
  ],
  "nextCursor": "eyJ2IjoxLCJzIjoiYWxsIiwidCI6..."
}
```

只返回公开帖子。翻页用 `nextCursor`：把它作为 `?cursor=` 再请求一次。

### `GET /openapi/v1/users/{handle}`

```json
{
  "user": {
    "id": "0193...",
    "name": "张三",
    "handle": "zhangsan",
    "bio": "...",
    "institution": "某大学",
    "researchFields": ["NLP", "多模态"],
    "verifyType": "influencer",
    "createdAt": "2026-01-01T00:00:00Z",
    "postCount": 42,
    "followerCount": 128,
    "followingCount": 36
  }
}
```

只返回可以对外展示的字段，不含 email、role、status 等账号信息。

## 错误

```json
{"error": {"code": "insufficient_scope", "message": "该 API Key 没有访问此资源的权限"}}
```

| 状态码 | code | 含义 |
|---|---|---|
| 401 | `unauthorized` | 缺少密钥、密钥无效、已撤销或已过期 |
| 403 | `insufficient_scope` | 密钥有效但缺少该端点所需的 scope |
| 404 | `not_found` | 资源不存在 |
| 422 | `validation_error` | 参数不合法（如 limit 超范围） |
| 429 | `rate_limited` | 超过每分钟配额 |

401 不区分「没带」「不存在」「已撤销」三种情况：
任何区分都会让攻击者能判断某个字符串是否曾经是有效密钥。

## 设计取舍

- **只读**。开放 API 不提供任何写操作，避免第三方程序误改用户数据。
- **结构稳定**。返回扁平 DTO，不复用内部 API 里可能随产品迭代的嵌套结构。
- **与内部 API 隔离**。独立前缀、独立认证，Cookie 与 CSRF 机制在这里不适用。
- **不加盐的 SHA-256**。密钥是 256 位随机数，不存在字典攻击空间；
  加盐会让「按哈希查表」退化为全表扫描。
