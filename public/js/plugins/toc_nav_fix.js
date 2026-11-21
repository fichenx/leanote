/**
 * 文档导航（右侧目录）点击跳转修复
 *
 * 问题：
 * 1. Markdown 模式下目录链接是普通锚点（href="#id"），但预览区位于
 *    .preview-container / #preview-contents 这类内部滚动容器中，
 *    浏览器默认的锚点跳转无法把标题滚入可视区域，点击看起来“没反应”。
 * 2. 富文本模式下内联 onclick 调用 page.js 的 scrollTo()，它用
 *    ":contains(文本)" 拼接选择器，标题含括号、引号等字符时会抛异常。
 *
 * 处理方式：捕获点击后按元素定位，直接滚动目标标题最近的滚动容器。
 */
(function () {
    'use strict';

    function decodeId(id) {
        try {
            return decodeURIComponent(id);
        } catch (e) {
            return id;
        }
    }

    // 预览区、编辑区都不是 window 滚动，需要找到最近的可滚动祖先
    function nearestScroller(el) {
        var node = el.parentNode;
        while (node && node.nodeType === 1 && node !== document.body) {
            var overflowY = window.getComputedStyle(node).overflowY;
            if ((overflowY === 'auto' || overflowY === 'scroll' || overflowY === 'overlay') &&
                node.scrollHeight > node.clientHeight + 2) {
                return node;
            }
            node = node.parentNode;
        }
        return null;
    }

    function scrollToElement(el) {
        if (!el) {
            return;
        }
        var scroller = nearestScroller(el);
        if (!scroller) {
            el.scrollIntoView();
            return;
        }
        var top = el.getBoundingClientRect().top - scroller.getBoundingClientRect().top +
            scroller.scrollTop - 12;
        scroller.scrollTop = Math.max(0, top);
    }

    // Markdown：右侧导航 + 正文中 [TOC] 生成的目录
    function markdownNavClick(e) {
        var link = $(e.target).closest('#leanoteNavContentMd a, #wmd-preview .toc a')[0];
        if (!link) {
            return false;
        }
        var href = link.getAttribute('href') || '';
        if (href.charAt(0) !== '#') {
            return false;
        }
        var rawId = href.slice(1);
        var target = document.getElementById(decodeId(rawId)) || document.getElementById(rawId);
        if (!target) {
            return false;
        }
        e.preventDefault();
        scrollToElement(target);
        return true;
    }

    // 富文本：右侧导航以 data-a 记录层级，按文字和同名标题序号定位，避免选择器注入
    function richNavClick(e) {
        var link = $(e.target).closest('#leanoteNavContent a[data-a]')[0];
        if (!link) {
            return false;
        }
        var tagName = (link.getAttribute('data-a') || '').split('-')[0];
        if (!tagName) {
            return false;
        }
        var text = $(link).text();
        var targets = $('#editorContent').find(tagName).filter(function () {
            return $(this).text() === text;
        });
        if (!targets.length) {
            return false;
        }
        var sameText = $('#leanoteNavContent a[data-a]').filter(function () {
            return $(this).text() === text;
        });
        var index = sameText.index(link);
        e.preventDefault();
        // 捕获阶段阻止内联 onclick 继续执行旧的 scrollTo
        e.stopPropagation();
        scrollToElement(targets[Math.max(0, index)]);
        return true;
    }

    document.addEventListener('click', function (e) {
        if (markdownNavClick(e)) {
            return;
        }
        richNavClick(e);
    }, true);
})();
