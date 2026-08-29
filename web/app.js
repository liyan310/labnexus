/* LabNexus 前端逻辑(原生 JS,对接全部后端 API) */
'use strict';

// ===== API 封装 =====
let token = localStorage.getItem('ln_token') || '';

async function api(path, { method = 'GET', body, form } = {}) {
  const headers = {};
  if (body) headers['Content-Type'] = 'application/json';
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch('/api' + path, {
    method, headers,
    body: form ? form : (body ? JSON.stringify(body) : undefined),
    credentials: 'same-origin',
  });
  if (res.status === 401 && token && !path.startsWith('/auth/')) {
    // 尝试 refresh 一次
    const r = await fetch('/api/auth/refresh', { method: 'POST', credentials: 'same-origin' });
    if (r.ok) {
      const d = await r.json();
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      return api(path, { method, body, form });
    }
    Auth.logout(true);
    throw new Error('登录已过期,请重新登录');
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
  return data;
}

function errMsg(e) {
  return e && e.message ? e.message : String(e);
}
function showMsg(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}
function esc(s) {
  if (s == null) return '';
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// ===== 认证 =====
const Auth = {
  switchTab(mode) {
    document.getElementById('login-form').classList.toggle('hidden', mode !== 'login');
    document.getElementById('register-form').classList.toggle('hidden', mode !== 'register');
    document.getElementById('tab-login').classList.toggle('active', mode === 'login');
    document.getElementById('tab-register').classList.toggle('active', mode === 'register');
    showMsg('login-msg', '');
  },
  async login(e) {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      const d = await api('/auth/login', { method: 'POST', body: Object.fromEntries(fd) });
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      this.enter();
    } catch (err) { showMsg('login-msg', errMsg(err)); }
  },
  async register(e) {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      const d = await api('/auth/register', { method: 'POST', body: Object.fromEntries(fd) });
      token = d.access_token;
      localStorage.setItem('ln_token', token);
      this.enter();
    } catch (err) { showMsg('login-msg', errMsg(err)); }
  },
  async logout(silent) {
    try { await api('/auth/logout', { method: 'POST' }); } catch (_) { /* ignore */ }
    if (!silent) { localStorage.removeItem('ln_token'); token = ''; location.reload(); }
  },
  async enter() {
    document.getElementById('login-view').classList.add('hidden');
    document.getElementById('app-view').classList.remove('hidden');
    try {
      const me = await api('/me');
      document.getElementById('current-user').textContent = me.user.display_name + ' (' + me.user.role + ')';
      // 经费模块仅 admin/supervisor 可见
      const showFin = me.user.role === 'admin' || me.user.role === 'supervisor';
      document.querySelectorAll('.finance-only').forEach(b => b.classList.toggle('hidden', !showFin));
    } catch (_) { /* ok */ }
    App.nav('feed');
  },
  init() {
    if (token) this.enter();
  },
};

// ===== 视图切换 =====
const App = {
  current: 'feed',
  nav(view) {
    this.current = view;
    document.querySelectorAll('.nav-item').forEach(b => b.classList.toggle('active', b.dataset.view === view));
    const main = document.getElementById('main');
    main.innerHTML = '<div class="empty">加载中…</div>';
    // 箭头函数包裹保留 this(直接取函数引用会导致 this 丢失)
    const fn = {
      feed: () => Feed.render(),
      space: () => Space.render(),
      resources: () => Resources.render(),
      projects: () => Projects.render(),
      tags: () => Tags.render(),
      finance: () => Finance.render(),
    }[view];
    if (fn) fn();
  },
  init() {
    Auth.init();
  },
};

// ===== 信息流 =====
const Feed = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <button class="btn primary" onclick="Editor.open()">✏️ 发帖 / 写笔记</button>
        <button class="btn" onclick="Feed.render()">刷新</button>
      </div>
      <div id="feed-list"></div>`;
    await this.load();
  },
  async load(sort = 'latest') {
    try {
      const d = await api('/feed?sort=' + sort);
      const list = document.getElementById('feed-list');
      if (!list) return;
      await getMyId();
      if (!d.documents.length) { list.innerHTML = '<div class="empty">还没有公开帖子,发第一篇吧!</div>'; return; }
      list.innerHTML = d.documents.map(doc => `
        <div class="card">
          <h3>${esc(doc.title)}</h3>
          <div class="meta">
            <span>👤 ${esc(doc.author ? doc.author.display_name : '?')}</span>
            <span>🕐 ${new Date(doc.created_at).toLocaleString()}</span>
            ${(doc.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}
          </div>
          <div class="content-preview">${esc((doc.content || '').slice(0, 200))}</div>
          <div class="actions">
            <button class="btn small" onclick="Feed.like('${doc.id}')">👍 ${doc.reactions_count || 0}</button>
            <button class="btn small" onclick="Feed.toggleComments('${doc.id}')">💬 ${doc.comments_count || 0}</button>
            <button class="btn small" onclick="Feed.view('${doc.id}')">查看</button>
            ${doc.author_id === getMyId() ? `<button class="btn small" onclick="Editor.edit('${doc.id}')">编辑</button>` : ''}
          </div>
          <div id="comments-${doc.id}" class="hidden"></div>
        </div>`).join('');
    } catch (e) { document.getElementById('feed-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async like(docId) {
    try {
      await api('/documents/' + docId + '/reactions', { method: 'POST', body: { emoji: '👍' } });
      this.load();
    } catch (e) { alert(errMsg(e)); }
  },
  async toggleComments(docId) {
    const box = document.getElementById('comments-' + docId);
    if (!box.classList.contains('hidden')) { box.classList.add('hidden'); return; }
    try {
      const d = await api('/documents/' + docId + '/comments');
      box.classList.remove('hidden');
      box.innerHTML = (d.comments || []).map(c => `
        <div class="comment"><span class="author">${esc(c.author ? c.author.display_name : '?')}</span> ${esc(c.content)}
          ${c.author_id === getMyId() ? `<button class="btn small danger" onclick="Feed.delComment('${c.id}','${docId}')">删</button>` : ''}
        </div>`).join('')
        + `<div class="row"><input id="comment-input-${docId}" placeholder="写评论…">
          <button class="btn small primary" onclick="Feed.comment('${docId}')">发送</button></div>`;
    } catch (e) { alert(errMsg(e)); }
  },
  async comment(docId) {
    const input = document.getElementById('comment-input-' + docId);
    if (!input.value.trim()) return;
    try {
      await api('/documents/' + docId + '/comments', { method: 'POST', body: { content: input.value } });
      this.toggleComments(docId);
      this.load();
    } catch (e) { alert(errMsg(e)); }
  },
  async delComment(commentId, docId) {
    try { await api('/comments/' + commentId, { method: 'DELETE' }); this.load(); } catch (e) { alert(errMsg(e)); }
  },
  async view(docId) {
    try {
      const d = await api('/documents/' + docId);
      const main = document.getElementById('main');
      main.innerHTML = `
        <div class="card">
          <button class="btn ghost" onclick="App.nav('feed')">← 返回</button>
          <h2>${esc(d.title)}</h2>
          <div class="meta"><span>👤 ${esc(d.author ? d.author.display_name : '?')}</span>
            <span>${d.visibility === 'public' ? '公开' : '私有'}</span>
            ${(d.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}</div>
          <div class="content-preview" style="white-space:pre-wrap">${esc(d.content)}</div>
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 我的空间 =====
const Space = {
  currentFolder: null,
  folders: [],
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>我的空间</h2>
        <button class="btn primary" onclick="Editor.open()">✏️ 新建文档</button>
        <button class="btn" onclick="Space.render()">刷新</button>
      </div>
      <div class="row">
        <input id="folder-name" placeholder="新目录名称"><button class="btn" onclick="Space.addFolder()">建目录</button>
      </div>
      <div class="row" style="align-items:flex-start">
        <div id="folder-tree" style="min-width:220px;border:1px solid var(--border);border-radius:8px;padding:8px"></div>
        <div id="space-docs" style="flex:1"></div>
      </div>`;
    await this.loadTree();
  },
  async loadTree() {
    try {
      const d = await api('/me/space');
      this.folders = flatten(d.folders || []);
      const tree = document.getElementById('folder-tree');
      tree.innerHTML = `<div class="tree-item ${!this.currentFolder ? 'active' : ''}" onclick="Space.selectFolder(null)">📂 全部文档</div>`
        + renderTree(d.folders || [], 0, this.currentFolder);
      await this.loadDocs();
    } catch (e) { alert(errMsg(e)); }
  },
  selectFolder(id) {
    this.currentFolder = id;
    this.loadTree();
  },
  async addFolder() {
    const name = document.getElementById('folder-name').value.trim();
    if (!name) return;
    try {
      await api('/me/folders', { method: 'POST', body: { name, parent_id: this.currentFolder } });
      document.getElementById('folder-name').value = '';
      this.loadTree();
    } catch (e) { alert(errMsg(e)); }
  },
  async renameFolder(id) {
    const f = this.folders.find(x => x.id === id);
    const name = prompt('新名称', f ? f.name : '');
    if (!name) return;
    try { await api('/me/folders/' + id, { method: 'PATCH', body: { name } }); this.loadTree(); } catch (e) { alert(errMsg(e)); }
  },
  async delFolder(id) {
    if (!confirm('删除该目录?')) return;
    try { await api('/me/folders/' + id, { method: 'DELETE' }); this.loadTree(); } catch (e) { alert(errMsg(e)); }
  },
  async loadDocs() {
    try {
      const q = this.currentFolder ? '?folder_id=' + this.currentFolder : '';
      const d = await api('/me/documents' + q);
      const box = document.getElementById('space-docs');
      if (!(d.documents || []).length) { box.innerHTML = '<div class="empty">该目录下暂无文档</div>'; return; }
      box.innerHTML = d.documents.map(doc => `
        <div class="card">
          <h3>${esc(doc.title)} <span class="tag-pill">${doc.visibility === 'public' ? '公开' : '私有'}</span></h3>
          <div class="meta">🕐 ${new Date(doc.created_at).toLocaleString()} ${(doc.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}</div>
          <div class="actions">
            <button class="btn small" onclick="Editor.edit('${doc.id}')">编辑</button>
            <button class="btn small danger" onclick="Space.delDoc('${doc.id}')">删除</button>
          </div>
        </div>`).join('');
    } catch (e) { alert(errMsg(e)); }
  },
  async delDoc(id) {
    if (!confirm('删除该文档?')) return;
    try { await api('/documents/' + id, { method: 'DELETE' }); this.loadDocs(); } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 编辑器 =====
// ===== 标签多选组件(发帖/资源共用) =====
// loadTagPicker(containerId, selectedIds?) 异步加载 /api/tags 并渲染 checkbox;
// selectedTagIds(containerId) 读取当前勾选 id 数组。
let tagCache = null;
async function loadTagPicker(containerId, selectedIds) {
  const container = document.getElementById(containerId);
  if (!container) return;
  try {
    if (!tagCache) tagCache = await api('/tags');
    const tags = tagCache.tags || [];
    const sel = new Set(selectedIds || []);
    if (!tags.length) {
      container.innerHTML = '<span class="tag-picker-empty">暂无标签,先到「标签」页创建</span>';
      return;
    }
    container.innerHTML = tags.map(t => `
      <label class="tag-check">
        <input type="checkbox" value="${esc(t.id)}" ${sel.has(t.id) ? 'checked' : ''}>
        <span class="tag-pill" style="background:${esc(t.color || '#3b82f6')};color:#fff">${esc(t.name)}</span>
      </label>`).join('');
  } catch (e) {
    container.innerHTML = '<span class="tag-picker-empty">标签加载失败</span>';
  }
}
function selectedTagIds(containerId) {
  const container = document.getElementById(containerId);
  if (!container) return [];
  return Array.from(container.querySelectorAll('input[type=checkbox]:checked')).map(i => i.value);
}
function resetTagCache() { tagCache = null; }

// ===== 文档编辑器 =====
const Editor = {
  open(folderId) {
    document.getElementById('editor-title').textContent = '新建文档';
    document.getElementById('doc-id').value = '';
    document.getElementById('doc-title').value = '';
    document.getElementById('doc-content').value = '';
    document.getElementById('doc-visibility').value = 'private';
    document.getElementById('doc-folder').value = folderId || Space.currentFolder || '';
    loadTagPicker('doc-tag-picker', []);
    showMsg('editor-msg', '');
    document.getElementById('editor-modal').classList.remove('hidden');
  },
  async edit(docId) {
    try {
      const d = await api('/documents/' + docId);
      document.getElementById('editor-title').textContent = '编辑文档';
      document.getElementById('doc-id').value = docId;
      document.getElementById('doc-title').value = d.title;
      document.getElementById('doc-content').value = d.content;
      document.getElementById('doc-visibility').value = d.visibility;
      document.getElementById('doc-folder').value = d.folder_id || '';
      loadTagPicker('doc-tag-picker', (d.tags || []).map(t => t.id));
      showMsg('editor-msg', '');
      document.getElementById('editor-modal').classList.remove('hidden');
    } catch (e) { alert(errMsg(e)); }
  },
  close() { document.getElementById('editor-modal').classList.add('hidden'); },
  async save() {
    const id = document.getElementById('doc-id').value;
    const payload = {
      title: document.getElementById('doc-title').value,
      content: document.getElementById('doc-content').value,
      visibility: document.getElementById('doc-visibility').value,
    };
    const folder = document.getElementById('doc-folder').value;
    const tags = selectedTagIds('doc-tag-picker');
    payload.tag_ids = tags; // 明确提交(含清空)
    try {
      if (id) {
        await api('/documents/' + id, { method: 'PATCH', body: payload });
      } else {
        if (folder) payload.folder_id = folder;
        await api('/me/documents', { method: 'POST', body: payload });
      }
      this.close();
      App.nav('feed');
    } catch (e) { showMsg('editor-msg', errMsg(e)); }
  },
};

// ===== 资源库 =====
const Resources = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>资源库</h2>
        <select id="res-type" onchange="Resources.load()">
          <option value="">全部类型</option><option value="link">链接</option>
          <option value="file">文件</option>
        </select>
        <input id="res-keyword" placeholder="关键词…" onkeydown="if(event.key==='Enter')Resources.load()" style="max-width:200px">
        <button class="btn" onclick="Resources.load()">筛选</button>
        <button class="btn" onclick="Resources.showCreate('link')">🔗 新建链接</button>
        <button class="btn primary" onclick="Resources.showCreate('file')">📤 上传文件</button>
      </div>
      <div id="res-list"></div>`;
    await this.load();
  },
  async load() {
    try {
      const type = document.getElementById('res-type').value;
      const kw = document.getElementById('res-keyword').value;
      const q = new URLSearchParams({ type, keyword: kw }).toString();
      const d = await api('/resources?' + q);
      const list = document.getElementById('res-list');
      if (!list) return;
      if (!d.resources.length) { list.innerHTML = '<div class="empty">暂无资源,上传或添加第一个吧</div>'; return; }
      list.innerHTML = d.resources.map(r => `
        <div class="card">
          <h3>${r.type === 'link' ? '🔗' : '📎'} ${esc(r.title)}</h3>
          <div class="meta">
            <span>类型:${r.type === 'link' ? '链接' : '文件'}</span>
            ${r.url ? `<span><a href="${esc(r.url)}" target="_blank" rel="noopener">${esc(r.url)}</a></span>` : ''}
            ${r.original_name ? `<span>📄 ${esc(r.original_name)}</span>` : ''}
            ${r.file_size ? `<span>大小:${fmtSize(r.file_size)}</span>` : ''}
            <span>上传:${esc(r.uploader ? r.uploader.display_name : '?')}</span>
            ${(r.tags || []).map(t => `<span class="tag-pill">${esc(t.name)}</span>`).join('')}
          </div>
          ${r.description ? `<div class="content-preview">${esc(r.description)}</div>` : ''}
          <div class="actions">
            ${r.download_url ? `<a class="btn" href="${esc(r.download_url)}" download>⬇ 下载</a>` : ''}
            ${r.preview && r.preview.supported ? `<button class="btn" onclick="Resources.preview('${r.id}')">👁 预览</button>` : ''}
            <button class="btn ghost" onclick="Resources.del('${r.id}')">删除</button>
          </div>
        </div>`).join('');
    } catch (e) { document.getElementById('res-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  showCreate(type) {
    type = type || 'link';
    document.getElementById('res-editor-title').textContent = type === 'link' ? '新建链接' : '上传文件';
    document.getElementById('res-type').value = type;
    document.getElementById('res-title').value = '';
    document.getElementById('res-url').value = '';
    document.getElementById('res-description').value = '';
    document.getElementById('res-file').value = '';
    document.getElementById('res-file').style.display = type === 'file' ? '' : 'none';
    document.getElementById('res-url').style.display = type === 'link' ? '' : 'none';
    loadTagPicker('res-tag-picker', []);
    showMsg('res-msg', '');
    document.getElementById('resource-modal').classList.remove('hidden');
    document.getElementById('res-type').onchange = () => {
      const t = document.getElementById('res-type').value;
      document.getElementById('res-editor-title').textContent = t === 'link' ? '新建链接' : '上传文件';
      document.getElementById('res-url').style.display = t === 'link' ? '' : 'none';
      document.getElementById('res-file').style.display = t === 'file' ? '' : 'none';
    };
  },
  closeCreate() { document.getElementById('resource-modal').classList.add('hidden'); },
  async saveCreate() {
    const type = document.getElementById('res-type').value;
    const title = document.getElementById('res-title').value.trim();
    const description = document.getElementById('res-description').value.trim();
    const tag_ids = selectedTagIds('res-tag-picker');
    try {
      if (type === 'link') {
        const url = document.getElementById('res-url').value.trim();
        await api('/resources', { method: 'POST', body: { type: 'link', title, url, description, tag_ids } });
      } else {
        const file = document.getElementById('res-file').files[0];
        if (!file) throw new Error('请选择文件');
        const form = new FormData();
        form.append('file', file);
        if (title) form.append('title', title);
        if (description) form.append('description', description);
        if (tag_ids.length) form.append('tag_ids', JSON.stringify(tag_ids));
        const res = await fetch('/api/resources/upload', {
          method: 'POST', headers: { 'Authorization': 'Bearer ' + token }, body: form, credentials: 'same-origin',
        });
        if (!res.ok) {
          const data = await res.json().catch(() => null);
          throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
        }
      }
      this.closeCreate();
      this.render();
    } catch (e) { showMsg('res-msg', errMsg(e)); }
  },
  async preview(id) {
    try {
      const d = await api('/resources/' + id);
      const r = d.resource;
      if (!r.preview || !r.preview.supported) return;
      // 新窗口打开预览(后端以 inline 返回,文本/图片/PDF/视频由浏览器渲染)
      window.open(r.preview.url, '_blank');
    } catch (e) { alert(errMsg(e)); }
  },
  async del(id) {
    if (!confirm('确定删除该资源?')) return;
    try { await api('/resources/' + id, { method: 'DELETE' }); this.load(); } catch (e) { alert(errMsg(e)); }
  },
};

function fmtSize(n) {
  if (n == null) return '';
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
  return (n / 1073741824).toFixed(1) + ' GB';
}

// ===== 项目 =====
const Projects = {
  current: null,
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>项目</h2>
        <button class="btn primary" onclick="Projects.create()">＋ 新建项目</button>
        <button class="btn" onclick="Projects.render()">刷新</button>
      </div>
      <div id="proj-list"></div>`;
    try {
      const d = await api('/projects');
      const list = document.getElementById('proj-list');
      if (!d.projects.length) { list.innerHTML = '<div class="empty">暂无项目</div>'; return; }
      list.innerHTML = d.projects.map(p => `
        <div class="card" style="cursor:pointer" onclick="Projects.open('${p.id}')">
          <h3>${esc(p.name)}</h3>
          <div class="meta"><span>负责人:${esc(p.owner ? p.owner.display_name : '?')}</span>
            <span>状态:${p.status}</span><span>🕐 ${new Date(p.created_at).toLocaleString()}</span></div>
          ${p.description ? `<div class="content-preview">${esc(p.description)}</div>` : ''}
        </div>`).join('');
    } catch (e) { document.getElementById('proj-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async create() {
    const name = prompt('项目名称');
    if (!name) return;
    try { await api('/projects', { method: 'POST', body: { name, description: '' } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async open(pid) {
    try {
      const d = await api('/projects/' + pid);
      this.current = d.project;
      const p = d.project;
      const main = document.getElementById('main');
      main.innerHTML = `
        <div class="toolbar"><h2>📋 ${esc(p.name)}</h2>
          <button class="btn ghost" onclick="Projects.render()">← 返回</button>
          <button class="btn" onclick="Projects.addMember()">＋ 加成员</button>
          <button class="btn" onclick="Projects.addMilestone()">🏁 里程碑</button>
          <button class="btn primary" onclick="Projects.addTask()">＋ 任务</button>
        </div>
        <div class="card"><div class="meta">
          <span>负责人:${esc(p.owner ? p.owner.display_name : '?')}</span>
          <span>成员:${(p.members || []).map(m => esc(m.user ? m.user.display_name : '?') + '(' + m.role + ')').join(', ')}</span>
        </div>
        <div class="meta">里程碑:${(p.milestones || []).map(m => esc(m.name) + (m.due_date ? ' 截止' + m.due_date : '')).join(' | ') || '无'}</div></div>
        <div class="board">${['todo', 'in_progress', 'blocked', 'done'].map(st => `
          <div class="board-col"><h4>${statusName(st)}</h4>
            ${(p.tasks || []).filter(t => t.status === st).map(t => `
              <div class="task-card">
                <div class="title">${esc(t.title)}</div>
                <div class="due">${t.due_date ? '⏰ ' + t.due_date : ''} ${t.priority === 'high' ? '🔴高' : t.priority === 'low' ? '🟢低' : ''}</div>
                <div class="meta">指派:${esc(t.assignee ? t.assignee.display_name : '未指派')}</div>
                <div class="actions">${transitionBtns(t)}</div>
              </div>`).join('') || '<div class="empty" style="padding:8px">空</div>'}
          </div>`).join('')}
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
  async addMember() {
    const uid = prompt('用户ID(从注册用户名查询:先用对方登录看 /api/me)');
    if (!uid) return;
    try { await api('/projects/' + this.current.id + '/members', { method: 'POST', body: { user_id: uid } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async addMilestone() {
    const name = prompt('里程碑名称');
    if (!name) return;
    const due = prompt('截止日期(YYYY-MM-DD,可空)') || null;
    try { await api('/projects/' + this.current.id + '/milestones', { method: 'POST', body: { name, due_date: due } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async addTask() {
    const title = prompt('任务标题');
    if (!title) return;
    try { await api('/projects/' + this.current.id + '/tasks', { method: 'POST', body: { title, priority: 'medium' } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
  async transition(taskId, status) {
    try { await api('/tasks/' + taskId + '/transition', { method: 'POST', body: { status } }); this.open(this.current.id); } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 经费管理(仅 admin/supervisor) =====
const Finance = {
  current: null, // 当前批次详情
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar">
        <h2>💰 经费管理</h2>
        <button class="btn primary" onclick="Finance.newBatch()">＋ 新建批次</button>
        <button class="btn" onclick="Finance.render()">刷新</button>
      </div>
      <div id="fin-ledger"></div>
      <div id="fin-list"></div>`;
    await Promise.all([this.loadLedger(), this.loadBatches()]);
  },
  async loadLedger() {
    try {
      const d = await api('/finance/ledger');
      const el = document.getElementById('fin-ledger');
      if (!el) return;
      el.innerHTML = `<div class="card" style="background:linear-gradient(135deg,#f0f9ff,#e0f2fe)">
        <h3>资金池余额:¥${fen2yuan(d.balance)}</h3>
        <div class="meta"><span>收入 − 支出</span>
        <button class="btn small" onclick="Finance.addIncome()">＋ 导师补充</button>
        <button class="btn small" onclick="Finance.addExpense()">－ 支出</button>
        <button class="btn small" onclick="Finance.showLedger()">流水明细</button></div>
      </div>`;
    } catch (e) { /* 403 等由列表统一处理 */ }
  },
  async loadBatches() {
    try {
      const d = await api('/finance/batches');
      const list = document.getElementById('fin-list');
      if (!list) return;
      if (!d.batches.length) { list.innerHTML = '<div class="empty">暂无批次,新建一个吧</div>'; return; }
      list.innerHTML = d.batches.map(b => `
        <div class="card" style="cursor:pointer" onclick="Finance.open('${b.id}')">
          <h3>📦 ${esc(b.name)} ${b.status === 'done' ? '<span class="tag-pill">已完成</span>' : '<span class="tag-pill">进行中</span>'}</h3>
          <div class="meta">
            <span>明细:${b.summary.item_count}</span>
            <span>应交:¥${fen2yuan(b.summary.total_should_return)}</span>
            <span>已交:¥${fen2yuan(b.summary.total_returned)}</span>
            <span style="color:${b.summary.total_unreturned > 0 ? 'var(--danger)' : 'inherit'}">未交:¥${fen2yuan(b.summary.total_unreturned)}</span>
          </div>
        </div>`).join('');
    } catch (e) {
      const list = document.getElementById('fin-list');
      if (list) list.innerHTML = `<div class="empty">${esc(errMsg(e))}(仅经费负责人/导师可见)</div>`;
    }
  },
  async newBatch() {
    const name = prompt('批次名称(如 2026-08)');
    if (!name) return;
    try { await api('/finance/batches', { method: 'POST', body: { name } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async open(batchId) {
    try {
      const d = await api('/finance/batches/' + batchId);
      this.current = d.batch;
      const b = d.batch;
      const main = document.getElementById('main');
      const items = (b.items || []);
      const unreturned = items.filter(i => i.unreturned > 0);
      main.innerHTML = `
        <div class="toolbar"><h2>📦 ${esc(b.name)}</h2>
          <button class="btn ghost" onclick="Finance.render()">← 返回</button>
          <button class="btn" onclick="Finance.addItem('${b.id}')">＋ 手动加明细</button>
          <button class="btn" onclick="Finance.importExcel('${b.id}')">📥 导入 Excel</button>
          <button class="btn" onclick="Finance.downloadTemplate()">📄 下载模板</button>
          ${b.status === 'active' ? `<button class="btn primary" onclick="Finance.complete('${b.id}')">✅ 批次完成</button>` : ''}
        </div>
        <div class="card"><div class="meta">
          <span>明细:${b.summary.item_count} 条</span>
          <span>发放总额:¥${fen2yuan(b.summary.total_payroll)}</span>
          <span>应交:¥${fen2yuan(b.summary.total_should_return)}</span>
          <span>已交:¥${fen2yuan(b.summary.total_returned)}</span>
          <span style="color:${b.summary.total_unreturned > 0 ? 'var(--danger)' : 'inherit'}">未交:¥${fen2yuan(b.summary.total_unreturned)}</span>
        </div></div>
        ${unreturned.length ? `<div class="card" style="border-color:var(--danger)"><h4>⏰ 未交名单(${unreturned.length})</h4>
          ${unreturned.map(i => `<div class="meta"><span>${esc(i.participant.name)}</span><span>欠 ¥${fen2yuan(i.unreturned)}</span>${i.note ? `<span>备注:${esc(i.note)}</span>` : ''}</div>`).join('')}
        </div>` : ''}
        <div class="card"><h4>明细(${items.length})</h4>
          <table class="fin-table">
            <thead><tr><th>姓名</th><th>学号</th><th>日期</th><th>应发</th><th>扣税</th><th>辛苦费</th><th>应交</th><th>已交</th><th>未交</th><th>状态</th><th>备注</th><th></th></tr></thead>
            <tbody>${items.map(i => `
              <tr>
                <td>${esc(i.participant.name)}</td>
                <td>${esc(i.participant.student_no)}</td>
                <td>${fmtDate(i.date)}</td>
                <td>${fen2yuan(i.payroll_amount)}</td>
                <td>${fen2yuan(i.tax_amount)}</td>
                <td>${fen2yuan(i.tip_amount)}</td>
                <td>${fen2yuan(i.should_return)}</td>
                <td>${fen2yuan(i.returned)}</td>
                <td style="color:${i.unreturned > 0 ? 'var(--danger)' : 'inherit'}">${fen2yuan(i.unreturned)}</td>
                <td>${itemStatusName(i.status)}</td>
                <td class="meta">${esc(i.note || '')}</td>
                <td>${i.unreturned > 0 ? `<button class="btn small" onclick="Finance.submit('${i.id}')">收款</button>` : '✓'}</td>
              </tr>`).join('') || '<tr><td colspan="12" class="empty">暂无明细</td></tr>'}
            </tbody>
          </table>
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
  async addItem(batchId) {
    const name = prompt('姓名'); if (!name) return;
    const student_no = prompt('学号'); if (!student_no) return;
    const date = prompt('发放日期(YYYY-MM-DD)'); if (!date) return;
    const payroll = prompt('应发(元)'); if (!payroll) return;
    const tax = prompt('扣税(元,可空)') || '0';
    const tip = prompt('辛苦费(元,可空)') || '0';
    const note = prompt('备注(可空)') || '';
    try {
      await api('/finance/batches/' + batchId + '/items', { method: 'POST', body: {
        name, student_no, date,
        payroll_amount: yuan2fen(payroll), tax_amount: yuan2fen(tax), tip_amount: yuan2fen(tip), note,
      } });
      this.open(batchId);
    } catch (e) { alert(errMsg(e)); }
  },
  async downloadTemplate() {
    try {
      const res = await fetch('/api/finance/import-template', {
        headers: { 'Authorization': 'Bearer ' + token }, credentials: 'same-origin',
      });
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const blob = await res.blob();
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'finance-import-template.xlsx';
      a.click();
      URL.revokeObjectURL(a.href);
    } catch (e) { alert(errMsg(e)); }
  },
  importExcel(batchId) {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.xlsx';
    input.onchange = async () => {
      const file = input.files[0];
      if (!file) return;
      const form = new FormData();
      form.append('file', file);
      try {
        const res = await fetch('/api/finance/batches/' + batchId + '/items/import-preview', {
          method: 'POST', headers: { 'Authorization': 'Bearer ' + token }, body: form, credentials: 'same-origin',
        });
        const data = await res.json().catch(() => null);
        if (!res.ok) throw new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
        const msg = `导入预览:有效 ${data.valid_count} 行,错误 ${data.error_count} 行\n\n${(data.error_rows || []).join('\n')}\n\n确认导入?`;
        if (!confirm(msg)) return;
        const c = await api('/finance/imports/' + data.preview_id + '/confirm?batch_id=' + batchId, { method: 'POST' });
        alert('导入完成:' + c.imported_count + ' 条');
        this.open(batchId);
      } catch (e) { alert(errMsg(e)); }
    };
    input.click();
  },
  async submit(itemId) {
    const amount = prompt('本次上交金额(元)');
    if (!amount) return;
    const date = prompt('上交日期(YYYY-MM-DD)') || new Date().toISOString().slice(0, 10);
    const note = prompt('备注(可空,如补交)') || '';
    try {
      await api('/finance/items/' + itemId + '/submit', { method: 'POST', body: {
        amount: yuan2fen(amount), date, note,
      } });
      this.open(this.current.id);
    } catch (e) { alert(errMsg(e)); }
  },
  async complete(batchId) {
    if (!confirm('标记批次完成?(需全部交清)')) return;
    try { await api('/finance/batches/' + batchId + '/complete', { method: 'POST' }); this.open(batchId); } catch (e) { alert(errMsg(e)); }
  },
  async addIncome() {
    const amount = prompt('导师补充金额(元)');
    if (!amount) return;
    const note = prompt('备注') || '';
    try { await api('/finance/ledger/income', { method: 'POST', body: { amount: yuan2fen(amount), date: today(), note } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async addExpense() {
    const amount = prompt('支出金额(元)');
    if (!amount) return;
    const note = prompt('用途备注') || '';
    try { await api('/finance/ledger/expense', { method: 'POST', body: { amount: yuan2fen(amount), date: today(), note } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async showLedger() {
    try {
      const d = await api('/finance/ledger');
      const main = document.getElementById('main');
      main.innerHTML = `<div class="toolbar"><h2>💰 资金流水</h2>
        <button class="btn ghost" onclick="Finance.render()">← 返回</button></div>
        <div class="card"><h3>余额:¥${fen2yuan(d.balance)}</h3></div>
        ${(d.transactions || []).map(t => `
          <div class="card">
            <div class="meta">
              <span class="tag-pill" style="${t.type === 'income' ? 'background:#dcfce7' : 'background:#fee2e2'}">
                ${t.type === 'income' ? '收入 +' : '支出 −'}${fen2yuan(t.amount)}</span>
              <span>${t.category === 'turnover' ? '上交回笼' : t.category === 'labor' ? '劳务发放' : '其他'}</span>
              <span>🕐 ${new Date(t.occurred_at).toLocaleString()}</span>
              <span>经手:${esc(t.operator ? t.operator.display_name : '?')}</span>
            </div>
            ${t.note ? `<div class="content-preview">${esc(t.note)}</div>` : ''}
          </div>`).join('') || '<div class="empty">暂无流水</div>'}`;
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 标签 =====
const Tags = {
  async render() {
    const main = document.getElementById('main');
    main.innerHTML = `
      <div class="toolbar"><h2>标签</h2>
        <input id="tag-name" placeholder="新标签名"><button class="btn primary" onclick="Tags.create()">创建</button>
      </div>
      <div id="tag-list"></div>`;
    try {
      const d = await api('/tags');
      const list = document.getElementById('tag-list');
      if (!d.tags.length) { list.innerHTML = '<div class="empty">暂无标签</div>'; return; }
      list.innerHTML = d.tags.map(t => `
        <div class="card" style="cursor:pointer" onclick="Tags.contents('${t.id}')">
          <span class="tag-pill" style="background:${esc(t.color)};color:#fff">${esc(t.name)}</span>
          <span class="meta">点击查看内容</span>
        </div>`).join('');
    } catch (e) { document.getElementById('tag-list').innerHTML = `<div class="empty">${esc(errMsg(e))}</div>`; }
  },
  async create() {
    const name = document.getElementById('tag-name').value.trim();
    if (!name) return;
    try { await api('/tags', { method: 'POST', body: { name } }); this.render(); } catch (e) { alert(errMsg(e)); }
  },
  async contents(tagId) {
    try {
      const d = await api('/tags/' + tagId + '/contents');
      const main = document.getElementById('main');
      main.innerHTML = `<button class="btn ghost" onclick="Tags.render()">← 返回</button>`
        + (d.documents || []).map(doc => `
          <div class="card"><h3>${esc(doc.title)}</h3>
            <div class="meta">👤 ${esc(doc.author ? doc.author.display_name : '?')} ${doc.visibility === 'public' ? '公开' : '私有'}</div></div>`).join('')
        || '<div class="empty">该标签下暂无内容</div>';
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 搜索 =====
const Search = {
  async run() {
    const q = document.getElementById('search-input').value.trim();
    if (!q) return;
    try {
      const d = await api('/search?q=' + encodeURIComponent(q));
      const main = document.getElementById('main');
      const docs = d.documents || [], ress = d.resources || [], tasks = d.tasks || [];
      main.innerHTML = `<div class="toolbar"><h2>搜索:"${esc(q)}"</h2></div>
        <div class="result-group"><h4>📄 文档 (${docs.length})</h4>
          ${docs.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">👤 ${esc(x.author ? x.author.display_name : '?')} ${x.visibility === 'public' ? '公开' : '私有'}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>
        <div class="result-group"><h4>📚 资源 (${ress.length})</h4>
          ${ress.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">类型:${x.type}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>
        <div class="result-group"><h4>📋 任务 (${tasks.length})</h4>
          ${tasks.map(x => `<div class="card"><h3>${esc(x.title)}</h3><div class="meta">状态:${statusName(x.status)}</div></div>`).join('') || '<div class="empty">无</div>'}
        </div>`;
    } catch (e) { alert(errMsg(e)); }
  },
};

// ===== 工具函数 =====
let myId = null;
async function getMyId() {
  if (myId) return myId;
  try {
    const d = await api('/me');
    myId = d.user.id;
    return myId;
  } catch (_) { return ''; }
}
function flatten(nodes, depth = 0) {
  let out = [];
  (nodes || []).forEach(n => { out.push(n); out = out.concat(flatten(n.children, depth + 1)); });
  return out;
}
function renderTree(nodes, depth, selected) {
  return (nodes || []).map(n => `
    <div>
      <div class="tree-item ${n.id === selected ? 'active' : ''}" style="padding-left:${8 + depth * 16}px"
           onclick="Space.selectFolder('${n.id}')">
        📁 ${esc(n.name)}
        <span>
          <button class="btn small" onclick="event.stopPropagation();Space.renameFolder('${n.id}')">改</button>
          <button class="btn small danger" onclick="event.stopPropagation();Space.delFolder('${n.id}')">删</button>
        </span>
      </div>
      ${n.children && n.children.length ? `<div class="tree-children">${renderTree(n.children, depth + 1, selected)}</div>` : ''}
    </div>`).join('');
}
function statusName(s) {
  return { todo: '待办', in_progress: '进行中', blocked: '受阻', done: '完成', active: '进行中' }[s] || s;
}
function itemStatusName(s) {
  return { pending: '未交', partial: '部分交', done: '已交清' }[s] || s;
}
// 金额:分 → 元字符串(如 240000 → "2400";12345 → "123.45")
function fen2yuan(fen) {
  if (fen == null) return '0';
  const neg = fen < 0;
  const abs = Math.abs(fen);
  const yuan = Math.floor(abs / 100);
  const rem = abs % 100;
  return (neg ? '-' : '') + yuan + (rem ? '.' + String(rem).padStart(2, '0').replace(/0$/, '') : '');
}
// 金额:元字符串 → 分(如 "2400" → 240000;"123.45" → 12345)
function yuan2fen(yuan) {
  const s = String(yuan).trim();
  const parts = s.split('.');
  let y = parseInt(parts[0] || '0', 10);
  if (isNaN(y)) y = 0;
  let f = 0;
  if (parts[1]) {
    const frac = parts[1].slice(0, 2).padEnd(2, '0');
    f = parseInt(frac, 10) || 0;
  }
  return y * 100 + f;
}
function today() {
  return new Date().toISOString().slice(0, 10);
}
// fmtDate 日期展示:兼容 "2026-08-22" 与 "2026-08-22T00:00:00Z",统一取前 10 位。
function fmtDate(d) {
  if (!d) return '';
  return String(d).slice(0, 10);
}
function transitionBtns(task) {
  const map = {
    todo: [['in_progress', '▶ 开始']],
    in_progress: [['blocked', '⛔ 受阻'], ['done', '✅ 完成']],
    blocked: [['todo', '↩ 重开'], ['in_progress', '▶ 继续']],
    done: [],
  };
  return (map[task.status] || []).map(([st, label]) =>
    `<button class="btn small" onclick="Projects.transition('${task.id}','${st}')">${label}</button>`).join('');
}

// 全局初始化
window.api = api; // API 封装暴露(e2e 测试与调试用)
window.Auth = Auth; window.App = App; window.Feed = Feed; window.Space = Space;
window.Editor = Editor; window.Resources = Resources; window.Projects = Projects;
window.Finance = Finance; window.Tags = Tags; window.Search = Search;
document.addEventListener('DOMContentLoaded', () => App.init());
