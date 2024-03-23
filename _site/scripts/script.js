function goos() {
    const platform = navigator.platform;
    if (!platform) {
        return '';
    }
    if (platform.indexOf('Win') !== -1) {
        return 'windows';
    }
    if (platform.indexOf('Mac') !== -1) {
        return 'darwin';
    }
    if (platform.indexOf('Linux') !== -1) {
        return 'linux';
    }
    if (platform.indexOf('FreeBSD') !== -1) {
        return 'freebsd';
    }
    if (platform.indexOf('OpenBSD') !== -1) {
        return 'openbsd';
    }
    if (platform.indexOf('SunOS') !== -1) {
        return 'solaris';
    }
    if (platform.indexOf('Android') !== -1) {
        return 'android';
    }
    return '';
}

// 'darwin' vs 'darwin' => true
// 'darwin' vs 'darwin,amd64' => true
// 'darwin,amd64' vs 'darwin,amd64' => true
// 'darwin,amd64' vs 'darwin' => false
// 'darwin,!amd64' vs 'darwin' => true
function matchesTags(tags, given) {
    const givenSet = new Set(given.split(','));
    loopTerm:
    for (const term of tags.split(' ')) {
        for (const q of term.split(',')) {
            if (q === '') {
                continue;
            }
            if (q.startsWith('!')) {
                if (givenSet.has(q.substring(1)))  {
                    continue loopTerm;
                }
            } else {
                if (!givenSet.has(q)) {
                    continue loopTerm;
                }
            }
        }
        return true;
    }
    return false;
}

function updateCode() {
    for (const e of document.querySelectorAll('pre')) {
        if (!e.dataset['codesrc']) {
            for (const code of e.querySelectorAll('code')) {
                addCommentStyle(code);
            }
            continue;
        }
        (e => {
            fetch(e.dataset['codesrc']).then(r => {
                return r.text();
            }).then(text => {
                if (e.dataset['codelinerange']) {
                    const m = e.dataset['codelinerange'].match(/^(\d+)(-(\d+)?)?$/);
                    start = parseInt(m[1], 10) - 1;
                    end = start + 1;
                    if (m.length >= 4) {
                        if (m[3] !== undefined) {
                            end = parseInt(m[3], 10);
                        } else if (m[2] === "-") {
                            end = undefined;
                        }
                    }
                    text = text.trimEnd();
                    const lines = text.split("\n");
                    text = lines.slice(start, end).join("\n");
                }

                const code = document.createElement('code');
                if (!e.dataset['codelinerange']) {
                    text = text.trim();
                }
                code.textContent = text;
                addCommentStyle(code);
                e.appendChild(code);
            });
        })(e);
    }
}

function addCommentStyle(code) {
    if (code.childNodes.length !== 1) {
        return;
    }
    const text = code.childNodes[0];
    if (text.nodeType !== Node.TEXT_NODE) {
        return;
    }
    code.textContent = '';
    for (const line of text.wholeText.split('\n')) {
        if (!/^\s*\/\//.test(line) && !/^\s*#(\s|$)/.test(line)) {
            code.appendChild(document.createTextNode(line + '\n'))
            continue;
        }
        const span = document.createElement('span');
        span.classList.add('comment');
        span.textContent = line + '\n'
        code.appendChild(span);
    }
}

let tocLevel = 4;

function disableTOC() {
    tocLevel = -1;
}

function setTOCLevel(n) {
    tocLevel = n;
}

function updateTOC() {
    let toc = document.querySelector('.toc');
    if (toc !== null) {
        toc.parentNode.removeChild(toc);
    }

    let query = [];
    for (let l = 2; l <= tocLevel; l++) {
        query.push(`article h${l}`);
    }
    if (query.length === 0) {
        return;
    }

    let headers = document.querySelectorAll(query.join(', '));
    const ids = new Map();
    for (const header of headers) {
        if (!header.offsetWidth) {
            header.id = '';
            continue;
        }

        // https://www.w3.org/TR/html51/dom.html#the-id-attribute
        // The value must be unique amongst all the IDs in the element’s home subtree and must contain at least one
        // character. The value must not contain any space characters.
        const id = header.textContent.replace(/\s/mg, '_');
        let count = 0;
        let idWithNum = id;
        if (ids.has(id)) {
            count = ids.get(id);
            idWithNum += '__' + count;
        }
        count++;
        ids.set(id, count);
        header.id = idWithNum;
    }
    headers = Array.prototype.filter.call(headers, e => {
        return e.offsetParent !== null;
    });
    if (headers.length === 0) {
        return;
    }

    // Create TOC tree.
    toc = document.createElement('div');
    toc.classList.add('toc');
    toc.classList.add('grid-container');
    const gridItem = document.createElement('div');

    gridItem.classList.add('grid-item-4');
    toc.appendChild(gridItem);

    const ul = document.createElement('ul');
    gridItem.appendChild(ul);
    const stack = [ul];

    let last = null;
    for (const header of headers) {
        if (last && last.tagName !== header.tagName) {
            const diff = parseInt(last.tagName.substring(1), 10) - parseInt(header.tagName.substring(1), 10);
            if (diff < 0) {
                const ul = document.createElement('ul');
                const lis = stack[stack.length - 1].querySelectorAll('li');
                lis[lis.length - 1].appendChild(ul);
                stack.push(ul);
            } else {
                for (let i = 0; i < diff; i++) {
                    stack.pop();
                }
            }
        }
        const li = document.createElement('li');
        const a = document.createElement('a');
        a.textContent = header.textContent;
        a.href = `#${header.id}`;
        li.appendChild(a);
        stack[stack.length - 1].appendChild(li);
        last = header;
    }

    const h2s = document.querySelectorAll('main h2');
    for (const h2 of h2s) {
        if (h2.offsetParent === null) {
            continue
        }
        h2.parentNode.insertBefore(toc, h2);
        return;
    }
}

function updateBody() {
    const input = document.querySelector('input#sidemenu');
    // input is null e.g. on the 404 page.
    if (input === null) {
        return;
    }
    if (input.checked) {
        document.body.style.overflow = 'hidden';
    } else {
        document.body.style.overflow = 'visible';
    }
}

function updateCSS() {
    // Trick to override vh unit for mobile platforms.
    // See https://css-tricks.com/the-trick-to-viewport-units-on-mobile/
    const vh = window.innerHeight * 0.01;
    document.documentElement.style.setProperty('--vh', `${vh}px`);
}

window.addEventListener('DOMContentLoaded', () => {
    updateCode();
    updateBody();
    updateCSS();
    updateTOC();

    const sidemenu = document.querySelector('input#sidemenu');
    if (sidemenu !== null) {
        sidemenu.addEventListener('change', updateBody);
    }

    if (typeof katex !== 'undefined') {
        for (const e of document.querySelectorAll('p.math')) {
            const div = document.createElement('div');
            const text = e.textContent;
            e.textContent = '';
            e.appendChild(div);
            katex.render(text, div, {
                displayMode: true,
                strict: true,
            });
        }
        for (const e of document.querySelectorAll('span.math')) {
            katex.render(e.textContent, e, {
                displayMode: false,
                strict: true,
            });
        }
    }

    // Twitter
    // https://developer.twitter.com/en/docs/twitter-for-websites/javascript-api/guides/set-up-twitter-for-websites
    if (document.querySelectorAll('blockquote.twitter-tweet').length > 0) {
        window.twttr = ((d, s, id) => {
            var js, fjs = d.getElementsByTagName(s)[0], t = window.twttr || {};
            if (d.getElementById(id)) {
                return t;
            }
            js = d.createElement(s);
            js.id = id;
            js.src = 'https://platform.twitter.com/widgets.js';
            fjs.parentNode.insertBefore(js, fjs);
            t._e = [];
            t.ready = f => {
                t._e.push(f);
            };
            return t;
        })(document, 'script', 'twitter-wjs');
    }
});

window.addEventListener('resize', () => {
    updateCSS();
});
