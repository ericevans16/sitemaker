# sitemaker

基于 Go 语言从零设计的极简、高扩展静态站点生成器，内置 **Blueprint（工业蓝图线框）** 主题，专为技术与个人博客打造。

## ✨ 核心特性

- 📐 **工业蓝图线框美学 (`themes/blueprint`)**：网格背景、45° 排线阴影、四角工程十字标记、Canvas 动态粒子雨幕、SVG 3D 等轴测方块波动、53 周活跃度热力图、明暗双主题丝滑切换。
- 🔌 **多阶段插件系统 (`pkg/plugin`)**：支持围栏代码块接管 (`FenceRenderer`)、Goldmark 扩展注入 (`MarkdownExtender`)、前端资源注入 (`AssetProvider`)、模版函数注册 (`TemplateFuncs`)、页面转换 (`PageTransformer`)、虚拟页面与文件生成 (`Generator`) 以及构建后钩子 (`PostBuildHook`)。
- 🏷️ **完整的分类体系与内容发现**：
  - **标签 (`/tags/`)** 与 **分类 (`/categories/`)** 自动聚合页
  - **按年归档 (`/archives/`)**
  - **零后端毫秒级全文搜索 (`/search/` + `/search-index.json`)**，支持快捷键 `/` 聚焦
  - **文章前后篇导航** 与 **基于标签/分类权重的相似文章推荐**
  - **草稿模式 (`draft: true`)**：日常构建自动过滤，`sitemaker serve -D` 本地预览
- 🎨 **主题与内容彻底解耦**：主题独立存放于 `themes/<name>/`，站点根目录可通过 `layouts/` 与 `static/` 随时按文件覆盖主题模版或追加自定义样式。
- 📡 **开发服务器热重载 (`sitemaker serve`)**：基于 `fsnotify` 递归监听 + SSE 自动刷新浏览器，无需手动刷新。
- 🌐 **SEO 与 AI 友好**：自动生成 `feed.xml` (RSS 2.0)、`sitemap.xml`、`robots.txt`、`llm.txt` 以及 Schema.org JSON-LD 结构化数据。

---

## 📁 目录结构

```text
.
├── _config.yaml                  # 站点配置（站点信息、导航菜单、分类、链接规则、插件开关）
├── cmd/sitemaker/                # CLI 入口
├── pkg/
│   ├── model/                    # 公共内容模型 (Site, Page, Taxonomy, Term)
│   └── plugin/                   # 公共插件接口与优先级注册表
├── internal/
│   ├── config/                   # 配置解析与校验
│   ├── content/                  # Markdown Frontmatter 解析与站点图谱构建
│   ├── markdown/                 # Goldmark 渲染管线（GFM、脚注、Chroma、中文锚点、折叠图版）
│   ├── theme/                    # 主题加载、模版覆盖与 CSS/JS Hash 打包压缩
│   ├── build/                    # 全站构建编排与 RSS/Sitemap/Search 生成器
│   ├── server/                   # 本地预览 HTTP Server + SSE 热重载
│   ├── watcher/                  # 递归文件变更监听
│   └── cli/                      # 命令行子命令实现
├── plugins/                      # 内置插件
│   ├── imagelayout/              # ```image-layout 结构化图片网格/瀑布流/轮播
│   ├── callout/                  # ```note / ```tip / ```warning 工程风提示框
│   └── friends/                  # ```friends 友链卡片网格
├── themes/blueprint/             # Blueprint 工业蓝图主题（layouts + static + styles）
├── content/
│   ├── posts/                    # 博客文章 (Markdown)
│   ├── about/                    # 关于页面（支持中英双语 about_CN.md / about_EN.md）
│   ├── friends/                  # 友链页面
│   └── logs/                     # 站点演进日志
└── static/                       # 站点级静态资源（头像、文章配图等）
```

---

## 🚀 常用命令

```bash
# 1. 编译并构建静态站点至 public/
make build
# 或：./sitemaker build [-D|--drafts] [-o public] [--no-minify]

# 2. 启动本地实时预览服务器 (http://localhost:8080，带文件监听与浏览器自动刷新)
make serve
# 预览包含草稿的文章：
make dev   # 等价于 ./sitemaker serve --drafts

# 3. 新建文章或独立页面
./sitemaker new my-first-post -t "文章标题" -c "Tech" -tags "Go,SSG"
./sitemaker new page notes/reading -t "Reading Notes"

# 4. 查看所有文章列表 / 已注册插件
make list
make plugins

# 5. 运行全套单元测试
make test
```

---

## 📝 文章 Frontmatter 规范

```yaml
---
title: "你的文章标题"
author: "ZiwenYe"
date: "2026-11-15"
updated_at: "2026-11-15"
location: "Hangzhou"
avatar: "/static/imgs/avatar.png"
category: "Tech"                  # 对应 _config.yaml 中的 categories
tags: ["Go", "Architecture"]      # 自动生成 /tags/<slug>/ 聚合页与文末相关推荐
draft: false                      # 设为 true 时仅在 sitemaker serve -D 下渲染
toc: true                         # 是否显示右侧章节目录
---
```

### 内置围栏语法扩展

````markdown
```tip 性能提示
sitemaker 在构建期自动计算本地图片宽高并打包压缩 CSS/JS。
```
````

支持 `note`、`tip`、`info`、`warning`、`caution`、`danger` 六种工程蓝图风格提示框，以及 `mermaid` 流程图和 `image-layout` 多图排版。

---

## 🔌 编写自定义插件

只需在 `plugins/` 下新建一个包并实现 [`plugin.Plugin`](pkg/plugin/plugin.go) 及任意扩展接口即可：

```go
package myplugin

import "sitemaker/pkg/plugin"

type p struct{}

func init() { plugin.Register(&p{}, 200) }

func (*p) Name() string { return "my-plugin" }

func (*p) RenderFence(lang, code string, ctx plugin.FenceContext) (string, bool, error) {
    if lang != "hello" {
        return "", false, nil
    }
    return "<div class=\"hello\">" + code + "</div>", true, nil
}
```

在 `_config.yaml` 的 `plugins:` 下即可按名称启用、禁用或传入配置字典。
