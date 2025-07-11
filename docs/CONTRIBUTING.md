# 文档贡献指南 | Contributing docs <!-- omit from toc -->

- [tl;dr](#tldr)
- [前置条件](#前置条件)
- [环境准备](#环境准备)
- [构建文档](#构建文档)
- [预览文档](#预览文档)
- [更新翻译](#更新翻译)
  - [同步翻译文件](#同步翻译文件)
  - [进行翻译](#进行翻译)
  - [预览翻译](#预览翻译)
- [文件清理](#文件清理)
- [报告问题](#报告问题)
- [Prerequisites](#prerequisites)
- [Setting up](#setting-up)
- [Building docs](#building-docs)
- [Previewing docs](#previewing-docs)
- [Translations](#translations)
  - [Updating translation files](#updating-translation-files)
  - [Translating](#translating)
  - [Previewing translated docs](#previewing-translated-docs)
- [Cleaning up](#cleaning-up)
- [Reporting issues](#reporting-issues)

> [!TIP]
>
> 下文的示例命令建议在[本目录 (docs)](./) 下执行。

## tl;dr

```sh
python -m pip install -r requirements.txt
secretflow-doctools update-translations --lang en
secretflow-doctools build --lang zh_CN --lang en
secretflow-doctools preview
```

## 前置条件

本项目使用 [Sphinx] 作为文档框架。你需要：

- [Python] >= 3.10

## 环境准备

执行：

```sh
python -m venv .venv
source .venv/bin/activate
python -m pip install -r requirements.txt
```

这将会：

- 在当前目录下的 `.venv` 目录创建一个 [Python 虚拟环境][venv]
- 激活该虚拟环境（以确保依赖被安装到正确位置）
- 安装文档构建[所需要的依赖](./requirements.txt)

> [!TIP]
>
> 这里以 Python 自带的 [`venv`][venv] 为示例。你也可以使用 [uv], [mamba] 等其他的依赖管
> 理工具。

## 构建文档

[`secretflow-doctools`] 是针对隐语项目文档构建的辅助工具，它协助开发者在本地构建并
且[预览](#预览文档)文档。

执行：

```sh
secretflow-doctools build --lang zh_CN --lang en
```

这将会构建中文版 `zh_CN` 以及英文版 `en` 文档。

如果一切正常，你应当能看到以下输出：

```log
SUCCESS  to preview, run: secretflow-doctools preview -c .
```

> [!TIP]
>
> 如果提示 `secretflow-doctools` 命令未找到，你可能没有执行 `source .venv/bin/activate`
> 以激活正确的 Python 环境；请参考[环境准备](#环境准备)中的指引。

如果想要只构建某个语言的文档，可以调整 `--lang` 选项。

## 预览文档

工具提供了本地预览的能力，帮助开发者验证文档在**发布到[隐语官网][website]后的显示效
果**。

执行：

```sh
secretflow-doctools preview
```

这将会在本地启动一个预览服务器。你应当能看到以下输出：

```
 * Running on http://127.0.0.1:5000
```

用浏览器访问 <http://127.0.0.1:5000> （或其它端口号），你应当能看到类似下图的页面，其中
将会列出在本地构建好的文档版本：

<figure>
  <img src="imgs/CONTRIBUTING/preview-sitemap.png" alt="the sitemap page">
</figure>

点击一个版本即可打开对应预览，你应当能看到类似下图的页面：

<figure>
  <img src="imgs/CONTRIBUTING/preview-content.png" alt="the content page">
</figure>

> [!TIP]
>
> 你可以保持预览服务器一直开启：在重新构建文档后，刷新页面即可看到更新的内容。

## 更新翻译

### 同步翻译文件

当你更新了文档原文（无论是新增、修改还是删除），你需要同时更新对应的翻译文件。

> [!IMPORTANT]
>
> 如果你的变更是对现有文本进行修改，请务必同步翻译文件，否则现有翻译可能会失效，发布后可
> 能会出现中英文夹杂的情况。

执行：

```sh
secretflow-doctools update-translations --lang en
# 其中 --lang en 代表目标语言是英文（翻译成英文）
```

这将会：

- 扫描文档原文的文本内容
- 更新 [locales/](locales/) 目录下的翻译文件，使得它们和原文维持同步

如果有更新，你应当能看到类似下文的输出，并且在版本控制中看到文件修改：

```
Update: locales/en/LC_MESSAGES/index.po +1, -0
...
SUCCESS  finished updating translation files
```

### 进行翻译

本项目使用 [GNU gettext][gettext] 来配置文档内容的多语言版本。翻译用文件全部位于
[locales/](locales/) 目录下。

[翻译文件（PO 文件）][gettext-po]的路径与文档原文是**一对一**的关系，示例：

|      |                                                                                                                |
| :--- | :------------------------------------------------------------------------------------------------------------- |
| 原文 | [**deployment/kuscia_monitor**.md](deployment/kuscia_monitor.md)                                               |
| 翻译 | [locales/en/LC_MESSAGES/**deployment/kuscia_monitor**.po](locales/en/LC_MESSAGES/deployment/kuscia_monitor.po) |

翻译文件的格式如下：

```gettext
msgid "你好，世界！"
msgstr "Hello, world!"
```

`msgid` 来自文档原文，**将会用于在构建时通过原文索引到翻译，因此请勿修改。**

`msgstr` 是翻译后的文本，请修改这一字段。

> [!TIP]
>
> [Poedit] 是一个用于编辑 PO 文件的自由开源软件。可以尝试使用它来进行翻译。
>
> <figure>
>   <img src="https://github.com/secretflow/doctools/raw/21e7d4d04f88c29fb68d9d668c6a74d43726eddf/tests/demo/media/poedit.png" alt="screenshot of Poedit">
> </figure>
> [!IMPORTANT]
>
> 以下是翻译的一些注意事项：

- `msgstr` 中可能会包含样式标记（比如字体加粗、链接等），在翻译文本中，标记应当与原文一
  致：

  ```diff
    msgid "这是一个 [链接](https://example.org/) 。"
  - msgstr "This is a `link <https://example.org/>`_."
  + msgstr "This is a [link](https://example.org/)."
  ```

- 在[同步翻译文件](#同步翻译文件)后，你可能会留意到一些翻译条目被加上了 `fuzzy` 的标记，
  比如：

  ```diff
    #: ../../topics/ccl/usage.rst:9
  + #, fuzzy
  - msgid "What is SCQL CCL? Please read :doc:`/topics/ccl/intro`."
  + msgid "What is SCQL CCL? Please read :doc:`/topics/ccl/intro` first."
    msgstr "什么是 SCQL CCL？请参阅 :doc:`/topics/ccl/intro`。"
  ```

  `fuzzy` 意味着 `gettext` 判断原文有轻微变化，需要人工校对。你应当更新翻译，然后将
  `fuzzy` 标记去掉。

  `fuzzy` 条目的意义在于构建后不会失效，也就是说，即使没有及时更新翻译，旧翻译也能继续显
  示（不过会稍微与原文不一致）。

- 你可能会在 [locales/](locales/) 目录下找到后缀为 `.mo` 的文件，这些文件时构建时产生的
  二进制文件，它们无法被修改也无需关注，你应当修改后缀为 `.po` 的文件。

### 预览翻译

在更新了翻译文件之后，重新[执行文档构建](#构建文档)然后[预览文档](#预览文档)，即可看到翻
译后的显示效果。

## 文件清理

以上流程会产生额外的临时文件，这些文件全部位于 [\_build](./_build/) 目录下。如果需要清理
，可以执行：

```sh
secretflow-doctools clean
```

## 报告问题

如果在以上过程中遇到报错、预览无法显示等问题，可以提交问题到
<https://github.com/secretflow/doctools/issues>。

文档内容及本项目代码的相关问题请提交到本项目的 Issues 中。

> [!NOTE]
>
> 为协助排查问题，你可以设置 `LOGURU_LEVEL=DEBUG` 环境变量来让文档工具输出更多日志。
>
> `secretflow-doctools` 会调用其他工具，在 `LOGURU_LEVEL=DEBUG` 时，日志会在每个步骤打印
> 完整的命令行指令：
>
> |                                           |                                |
> | :---------------------------------------- | :----------------------------- |
> | `secretflow-doctools build`               | [`sphinx-build`][sphinx-build] |
> | `secretflow-doctools update-translations` | [`sphinx-intl`]                |

---

> [!TIP]
>
> Command line examples in this guide assume your working directory to be [docs/](./).

## Prerequisites

This project uses [Sphinx] to build documentation. You will need:

- [Python] >= 3.10

## Setting up

Run:

```sh
python -m venv .venv
source .venv/bin/activate
python -m pip install -r requirements.txt
```

This will:

- Create a new [virtual environment][venv] in a `.venv` directory
- Activate the virtual environment (so that dependencies are installed to the correct
  environment).
- Install the [required dependencies](./requirements.txt)

> [!TIP]
>
> This example uses Python's built-in [`venv`][venv] module. You are free to use other
> package managers such as [uv] or [mamba].

## Building docs

[`secretflow-doctools`] is a command-line utility for building docs for SecretFlow
projects.

To build docs, run:

```sh
secretflow-doctools build --lang zh_CN --lang en
```

This will build both the Simplified Chinese (`zh_CN`) and the English (`en`) versions of
the documentation.

You should be able to see the following output:

```log
SUCCESS  to preview, run: secretflow-doctools preview -c .
```

> [!TIP]
>
> If you are getting a "command not found" error, you might not have activated the
> correct environment by running `source .venv/bin/activate`. Please review
> [Setting up](#setting-up).

## Previewing docs

The utility features a documentation previewer. You will be able to visualize how your
changes will eventually appear on our [website].

To preview the docs that was built, run:

```sh
secretflow-doctools preview
```

This will start the server. You should be able to see the following output:

```
 * Running on http://127.0.0.1:5000
```

Navigate to <http://127.0.0.1:5000> on your browser (the port number may be different),
you should be able to see a page similar to the following:

<figure>
  <img src="imgs/CONTRIBUTING/preview-sitemap.png" alt="the sitemap page">
</figure>

Click on a version to see the preview. You should be able to see a page similar to the
following:

<figure>
  <img src="imgs/CONTRIBUTING/preview-content.png" alt="the content page">
</figure>

> [!TIP]
>
> You may leave the preview server running. When you run the build command again,
> refresh the page to see updated content.

## Translations

### Updating translation files

After updating source docs, you should also update the corresponding translation files.

> [!IMPORTANT]
>
> If your updates involve rewriting existing texts, you MUST update translation files,
> otherwise some translated paragraphs may fall back to showing the original text.

Run:

```sh
secretflow-doctools update-translations --lang en
# `--lang en` sets the target language to English (i.e. you are translating into English)
```

This will:

- Scan source text for changes
- Update the translation files under [locales/](locales/) such that they become in-sync
  again

If there are updates, you should be able to see output similar to the following, and see
changes in source control:

```
Update: locales/en/LC_MESSAGES/index.po +1, -0
...
SUCCESS  finished updating translation files
```

### Translating

This project uses [GNU gettext][gettext] to translate docs during build. Translation
files are under [locales/](locales/).

Paths to the [translation files (PO files)][gettext-po] mirror their source document
counterparts. For example:

|              |                                                                                                                |
| :----------- | :------------------------------------------------------------------------------------------------------------- |
| Source texts | [**deployment/kuscia_monitor**.md](deployment/kuscia_monitor.md)                                               |
| Translations | [locales/en/LC_MESSAGES/**deployment/kuscia_monitor**.po](locales/en/LC_MESSAGES/deployment/kuscia_monitor.po) |

PO files have the following syntax:

```gettext
msgid "你好，世界！"
msgstr "Hello, world!"
```

`msgid` comes from source docs, which will be used to lookup translations during build,
and you should not modify them.

`msgstr` is the translation. Please update these.

> [!TIP]
>
> [Poedit] is a free and open-source graphical editor for gettext PO files and is highly
> recommended.
>
> <figure>
>   <img src="https://github.com/secretflow/doctools/raw/21e7d4d04f88c29fb68d9d668c6a74d43726eddf/tests/demo/media/poedit.png" alt="screenshot of Poedit">
> </figure>
> [!IMPORTANT]

- `msgstr` may contain inline markups, such as bolded text or links. Translations should
  retain such markups. You should ensure the markup syntax is consistent with the source
  document:

  ```diff
    msgid "这是一个 [链接](https://example.org/) 。"
  - msgstr "This is a `link <https://example.org/>`_."
  + msgstr "This is a [link](https://example.org/)."
  ```

- You may notice a `fuzzy` label after
  [updating translation files](#updating-translation-files):

  ```diff
    #: ../../topics/ccl/usage.rst:9
  + #, fuzzy
  - msgid "What is SCQL CCL? Please read :doc:`/topics/ccl/intro`."
  + msgid "What is SCQL CCL? Please read :doc:`/topics/ccl/intro` first."
    msgstr "什么是 SCQL CCL？请参阅 :doc:`/topics/ccl/intro`。"
  ```

  `fuzzy` indicates that a source paragraph is updated but only slightly. You should
  revise `fuzzy` translations, and then remove the `fuzzy` label.

  `fuzzy` entries will still appear in the output even if they not updated in time,
  albeit the displayed content is then slightly different from source text.

- You may notice `.mo` files under [locales/](locales/). These are binary files
  autogenerated during builds and are not editable. The files to edit are `.po` files.

### Previewing translated docs

After updating translations, [build docs](#building-docs) again to preview them.

## Cleaning up

The above tasks generate temporary files under [\_build](./_build/). To clean up these
files, run:

```sh
secretflow-doctools clean
```

## Reporting issues

If commands or previews aren't working as expecting, please file an issue at
<https://github.com/secretflow/doctools/issues>.

For project-specific questions, please file an issue in this repository instead.

> [!NOTE]
>
> To help with troubleshooting, set the `LOGURU_LEVEL=DEBUG` environment variable to see
> debug logs.
>
> `secretflow-doctools` invokes other programs. When `LOGURU_LEVEL=DEBUG` is set,
> logging will contain the full commands being run:
>
> | Command                                   | Delegates to                   |
> | :---------------------------------------- | :----------------------------- |
> | `secretflow-doctools build`               | [`sphinx-build`][sphinx-build] |
> | `secretflow-doctools update-translations` | [`sphinx-intl`]                |

[`secretflow-doctools`]: https://github.com/secretflow/doctools
[mamba]: https://mamba.readthedocs.io/en/latest/
[Poedit]: https://poedit.net/
[Python]: https://www.python.org/
[Sphinx]: https://www.sphinx-doc.org/en/master/tutorial/index.html
[uv]: https://docs.astral.sh/uv/
[venv]: https://docs.python.org/3/library/venv.html
[gettext]: https://www.gnu.org/software/gettext/
[gettext-po]: https://www.gnu.org/software/gettext/manual/html_node/PO-Files.html
[sphinx-build]: https://www.sphinx-doc.org/en/master/man/sphinx-build.html
[`sphinx-intl`]: https://www.sphinx-doc.org/en/master/usage/advanced/intl.html
[website]: https://www.secretflow.org.cn/
