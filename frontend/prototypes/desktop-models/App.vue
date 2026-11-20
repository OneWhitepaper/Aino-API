<template>
  <a class="skip-link" href="#model-catalog">跳转到模型目录</a>
  <div class="admin-shell">
    <aside class="admin-sidebar" :class="{ 'mobile-open': sidebarOpen }" aria-label="管理导航">
      <a class="brand" href="#model-catalog" @click="sidebarOpen = false">
        <img src="/omniflow-icon.png" alt="" width="34" height="34" />
        <span>OmniFlow <span class="brand-api">API</span><small>万流 · 管理控制台</small></span>
      </a>
      <div class="workspace-picker"><span class="workspace-mark">O</span><span>OmniFlow<small>管理工作空间</small></span><Icon name="chevronDown" /></div>
      <p class="nav-caption">工作空间</p>
      <nav>
        <template v-for="item in nav" :key="item.label">
          <a v-if="item.active" class="nav-item active" href="#model-catalog" aria-current="page" @click="sidebarOpen = false"><Icon :name="item.icon" /><span>{{ item.label }}</span><span class="nav-new">新增</span></a>
          <span v-else class="nav-item nav-context" :title="`${item.label}不在本次交互预览范围内`"><Icon :name="item.icon" /><span>{{ item.label }}</span><Icon v-if="item.label === '渠道管理'" class="nav-chevron" name="chevronRight" /></span>
        </template>
      </nav>
      <div class="sidebar-bottom">
        <div class="preview-note"><span class="status-dot" /> 本地交互预览 <span class="version">v1</span></div>
        <div class="admin-profile"><span class="avatar">管</span><div>站点管理员<small>OmniFlow API</small></div><span class="profile-symbol">⌘</span></div>
      </div>
    </aside>
    <button v-if="sidebarOpen" class="sidebar-scrim" aria-label="关闭管理导航" @click="sidebarOpen = false" />

    <div class="workspace-main">
      <header class="workspace-topbar">
        <div class="breadcrumb"><button class="icon-button mobile-menu" aria-label="打开管理导航" :aria-expanded="sidebarOpen" @click="sidebarOpen = !sidebarOpen"><Icon name="grid" /></button><span>管理控制台</span><Icon name="chevronRight" /><strong>内置模型</strong></div>
        <div class="topbar-tools"><span class="local-badge"><span />交互预览</span><button class="icon-button" aria-label="打开使用说明" @click="helpOpen = true"><Icon name="lightbulb" /></button></div>
      </header>

      <main id="model-catalog" class="catalog-main" tabindex="-1">
        <section class="catalog-workspace hero" data-enter aria-labelledby="catalog-title"><div class="catalog-wrap wrap">
          <div class="page-heading">
            <div><div class="eyebrow">模型与接入</div><div class="heading-title"><h1 id="catalog-title" data-enter-whole>内置模型</h1><span class="heading-count">{{ settings.models.length }}</span></div><p class="page-description">管理 Aino 桌面端的模型目录、调用方式与默认选择。</p></div>
            <div class="heading-actions"><button class="btn btn-secondary" data-testid="preview-open" @click="desktopPreviewOpen = true"><Icon name="eye" />预览桌面端</button><button class="btn btn-primary" data-testid="add-model" @click="openEditor()"><Icon name="plus" />添加模型</button></div>
          </div>

          <div class="connection-band">
            <div class="connection-identity"><div class="aino-icon"><img src="/aino-app-icon.png" alt="" width="30" height="30" /></div><div><h2>Aino 桌面端 <span class="small-status" :class="{ off: !settings.enabled }"><span />{{ settings.enabled ? '目录已启用' : '目录已关闭' }}</span></h2><p>文本与生图分别管理，使用已有分组和计费规则。</p></div></div>
            <div class="connection-actions"><button class="text-button" data-testid="connection-settings" @click="connectionOpen = true"><Icon name="cog" />接入设置</button><span class="vertical-rule" /><span class="switch-caption">开放模型目录</span><button type="button" class="switch" role="switch" :aria-checked="settings.enabled" aria-label="开放模型目录" data-testid="catalog-enabled" @click="settings.enabled = !settings.enabled"><span /></button></div>
          </div>

          <div v-if="!settings.enabled" class="inline-notice" role="status"><Icon name="eyeOff" /><span>目录已关闭。模型配置保留，保存后桌面端将停止展示内置模型。</span></div>

          <div class="catalog-panel app-frame">
            <div class="catalog-tabs-row">
              <div class="catalog-tabs" role="tablist" aria-label="模型目录类型">
                <button v-for="tab in tabs" :id="`tab-${tab.purpose}`" :key="tab.purpose" role="tab" :aria-selected="purpose === tab.purpose" aria-controls="catalog-panel" :tabindex="purpose === tab.purpose ? 0 : -1" :data-testid="`tab-${tab.purpose}`" @click="changePurpose(tab.purpose)" @keydown.right.prevent="changePurpose(purpose === 'chat' ? 'image' : 'chat', true)" @keydown.left.prevent="changePurpose(purpose === 'chat' ? 'image' : 'chat', true)" @keydown.home.prevent="changePurpose('chat', true)" @keydown.end.prevent="changePurpose('image', true)"><Icon :name="tab.icon" />{{ tab.label }}<span>{{ settings.models.filter(model => purposeOf(model) === tab.purpose).length }}</span></button>
              </div>
              <label class="default-picker"><span>默认{{ purpose === 'chat' ? '文本' : '生图' }}模型</span><select v-model="currentDefault" aria-label="默认模型" data-testid="default-model"><option value="">由用户选择</option><option v-for="model in defaultOptions" :key="model.id" :value="model.id">{{ model.display_name }}</option></select></label>
            </div>

            <div class="catalog-toolbar">
              <div class="filters"><label class="search-field"><Icon name="search" /><input ref="searchInput" v-model="search" type="search" aria-label="搜索模型" placeholder="搜索模型名称、ID 或分组…" data-testid="model-search" /><kbd>/</kbd></label><label class="filter-field"><select v-model="groupFilter" aria-label="筛选分组"><option value="all">全部分组</option><option v-for="group in availableGroups" :key="group.id" :value="String(group.id)">{{ group.name }}</option></select></label><label v-if="purpose === 'chat'" class="filter-field status-filter"><select v-model="statusFilter" aria-label="筛选验证状态"><option value="all">全部状态</option><option value="verified">已验证</option><option value="pending">待验证</option></select></label></div>
              <button class="btn btn-secondary import-button" data-testid="import-open" @click="openImporter"><Icon name="arrowDown" />从分组导入</button>
            </div>

            <div v-if="selected.size" class="selection-bar"><span>已选择 <strong>{{ selected.size }}</strong> 个模型</span><button class="text-button" @click="selected = new Set()">取消选择</button><button class="text-button danger-text" @click="requestRemove([...selected])"><Icon name="trash" />移出目录</button></div>

            <div id="catalog-panel" role="tabpanel" :aria-labelledby="`tab-${purpose}`" class="model-table-wrap">
              <span id="select-row-prefix" class="sr-only">选择</span><span id="select-all-prefix" class="sr-only">选择当前筛选下的全部</span><table class="model-table">
                <thead><tr><th class="selection-column"><input type="checkbox" aria-label="选择当前筛选下的全部模型" aria-labelledby="select-all-prefix model-column-label" :checked="allSelected" :indeterminate="selected.size > 0 && !allSelected" @change="toggleAll" /></th><th class="model-name-column"><span id="model-column-label">模型</span></th><th class="group-column">关联分组</th><th class="capability-column">{{ purpose === 'chat' ? '模型能力' : '质量档位' }}</th><th class="verification-column">{{ purpose === 'chat' ? '验证状态' : '调用协议' }}</th><th class="row-actions-column"><span class="sr-only">模型操作</span></th></tr></thead>
                <tbody>
                  <tr v-for="model in filteredModels" :key="model.id" :class="{ 'row-selected': selected.has(model.id) }" :data-model-id="model.id">
                    <td class="selection-column"><input type="checkbox" :aria-label="`选择 ${model.display_name}`" :aria-labelledby="`select-row-prefix model-title-${model.id}`" :checked="selected.has(model.id)" @change="toggleSelected(model.id)" /></td>
                    <td class="model-name-column"><div class="model-identity"><div class="provider-icon" :class="`provider-${providerKey(model)}`"><img :src="providerIcon(model)" alt="" width="24" height="24" loading="lazy" decoding="async" /></div><div class="model-details"><button class="model-name" :id="`model-title-${model.id}`" :data-testid="`edit-${model.id}`" @click="openEditor(model)">{{ model.display_name }}<span v-if="currentDefault === model.id" class="default-tag"><Icon name="check" />默认</span></button><code>{{ model.model }}</code></div></div></td>
                    <td class="group-column"><span class="group-label"><span class="group-square" />{{ groupName(model.group_id) }}</span><small>{{ protocolLabels[model.api_mode] }}</small></td>
                    <td class="capability-column"><div class="capability-list" v-if="purpose === 'chat'"><span v-if="model.capabilities.tools" title="支持工具调用">工具</span><span v-if="model.capabilities.vision" title="支持图片输入">视觉</span><span v-if="model.capabilities.reasoning" title="支持推理">推理</span><span v-if="!model.capabilities.tools && !model.capabilities.vision && !model.capabilities.reasoning">未配置</span></div><div v-else class="capability-list"><span v-for="quality in (model.capabilities.image_quality_options || []).slice(0, 3)" :key="quality">{{ qualityLabels[quality] || quality }}</span><span v-if="(model.capabilities.image_quality_options || []).length > 3">+{{ model.capabilities.image_quality_options!.length - 3 }}</span><span v-if="!model.capabilities.image_quality_options?.length">模型默认</span></div></td>
                    <td class="verification-column"><span v-if="purpose === 'chat'" class="verification-badge" :class="model.agent_verified ? 'verified' : 'pending'"><Icon :name="model.agent_verified ? 'checkCircle' : 'clock'" />{{ model.agent_verified ? '已验证' : '待验证' }}</span><span v-else class="verification-badge neutral"><Icon name="grid" />Images API</span></td>
                    <td class="row-actions-column"><div class="row-actions"><button class="icon-button default-action" :class="{ 'is-default': currentDefault === model.id }" :aria-label="currentDefault === model.id ? `${model.display_name} 已是默认模型` : `将 ${model.display_name} 设为默认`" :title="!canDefault(model) ? '验证工具调用和流式回复后才能设为默认' : '设为默认模型'" :disabled="!canDefault(model) || currentDefault === model.id" @click="setDefault(model)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="m12 3 2.8 5.7 6.3.9-4.6 4.5 1.1 6.2L12 17.4l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9Z" stroke-linejoin="round" /></svg></button><button class="icon-button" :aria-label="`编辑 ${model.display_name}`" @click="openEditor(model)"><Icon name="edit" /></button><button class="icon-button remove-action" :aria-label="`移除 ${model.display_name}`" @click="requestRemove([model.id])"><Icon name="more" /></button></div></td>
                  </tr>
                </tbody>
              </table>
              <div v-if="!filteredModels.length" class="empty-state"><div><Icon name="search" /></div><h3>没有匹配的模型</h3><p>试试其他关键词，或清除分组与状态筛选。</p><button class="btn btn-secondary" @click="resetFilters">清除筛选</button></div>
            </div>

            <div class="table-footer"><span>显示 {{ filteredModels.length }} 个{{ purpose === 'chat' ? '文本' : '生图' }}模型<span v-if="purpose === 'chat'" class="footer-divider">·</span><span v-if="purpose === 'chat'">{{ activeModels.filter(model => model.agent_verified).length }} 个已验证</span></span><span class="sort-hint"><Icon name="arrowsUpDown" />按显示顺序排列</span></div>
          </div>

          <div class="catalog-guidance"><Icon name="shield" /><p>模型价格、用户倍率与使用权限沿用已有分组配置。<span>文本模型需验证工具调用与流式回复，才能设为默认。</span></p><button class="text-button" @click="helpOpen = true">查看配置说明<Icon name="arrowRight" /></button></div>
          <div class="page-footnote"><span>模型目录 · Aino 桌面端</span><span>配置变更保存在当前浏览器</span></div>
        </div></section>
      </main>

      <Transition name="savebar"><div v-if="dirty" class="unsaved-bar" role="status"><span><span class="unsaved-dot" />{{ savedMessage || '有未保存的配置更改' }}<small>保存至本地预览</small></span><div><button class="btn btn-secondary" @click="discardOpen = true">放弃更改</button><button class="btn btn-primary" data-testid="save-catalog" @click="saveSettings"><Icon name="check" />保存配置</button></div></div></Transition>
    </div>
  </div>

  <PreviewOverlay :open="!!draft" :label="draft?.previousID ? '编辑模型' : '添加模型'" variant="drawer" @close="closeEditor">
    <template v-if="draft">
      <header class="panel-header"><div><span class="panel-eyebrow">{{ purposeOf(draft.entry) === 'chat' ? '文本模型' : '生图模型' }}</span><h2>{{ draft.previousID ? '编辑模型' : '添加模型' }}</h2><p>{{ draft.previousID ? draft.entry.display_name : '将模型加入 Aino 桌面端目录。' }}</p></div><button class="icon-button" aria-label="关闭模型编辑" @click="closeEditor"><Icon name="x" /></button></header>
      <form class="editor-form" novalidate @submit.prevent="saveEntry">
        <div class="panel-scroll">
          <div class="editor-section-title"><span>01</span><h3>基本信息</h3></div>
          <div class="field"><label for="model-display-name">显示名称 <span>*</span></label><input id="model-display-name" v-model="draft.entry.display_name" data-testid="editor-name" autocomplete="off" :aria-invalid="!!errors.display_name" :aria-describedby="errors.display_name ? 'error-display_name' : undefined" placeholder="例如 DeepSeek Chat" /><small v-if="errors.display_name" id="error-display_name" class="field-error">{{ errors.display_name }}</small></div>
          <div class="field"><label for="model-upstream-id">实际模型 ID <span>*</span></label><input id="model-upstream-id" v-model="draft.entry.model" class="mono-input" data-testid="editor-model" :aria-invalid="!!errors.model" :aria-describedby="errors.model ? 'error-model' : undefined" placeholder="deepseek-chat" /><small v-if="errors.model" id="error-model" class="field-error">{{ errors.model }}</small><small v-else>发送到上游接口的模型名称。</small></div>
          <div class="field-grid"><div class="field"><label for="catalog-id">目录 ID <span>*</span><span v-if="draft.previousID" class="field-locked">已固定</span></label><input id="catalog-id" v-model="draft.entry.id" class="mono-input" :readonly="!!draft.previousID" :aria-invalid="!!errors.id" :aria-describedby="errors.id ? 'error-id' : undefined" placeholder="deepseek-chat" /><small v-if="errors.id" id="error-id" class="field-error">{{ errors.id }}</small><small v-else>用于恢复会话，添加后保持稳定。</small></div><div class="field"><label for="provider-label">供应商名称 <span>*</span></label><input id="provider-label" v-model="draft.entry.provider_label" :aria-invalid="!!errors.provider_label" :aria-describedby="errors.provider_label ? 'error-provider_label' : undefined" placeholder="DeepSeek" /><small v-if="errors.provider_label" id="error-provider_label" class="field-error">{{ errors.provider_label }}</small></div></div>

          <div class="editor-section-title"><span>02</span><h3>调用配置</h3></div>
          <div class="field"><label for="model-group">关联分组 <span>*</span></label><select id="model-group" :value="draft.entry.group_id" :aria-invalid="!!errors.group_id" :aria-describedby="errors.group_id ? 'error-group_id' : undefined" @change="setDraftGroup"><option :value="0" disabled>选择已有分组</option><option v-for="group in editorGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select><small v-if="errors.group_id" id="error-group_id" class="field-error">{{ errors.group_id }}</small><small v-else>权限与价格沿用该分组；调用不绑定单个上游账号。</small></div>
          <div class="field-grid"><div class="field"><label for="model-platform">实际模型平台</label><select id="model-platform" v-model="draft.entry.platform"><option v-for="platform in editorPlatforms" :key="platform" :value="platform">{{ platform }}</option></select></div><div class="field"><label for="model-protocol">调用协议</label><select id="model-protocol" v-model="draft.entry.api_mode"><option v-for="protocol in editorProtocols" :key="protocol" :value="protocol">{{ protocolLabels[protocol] }}</option></select></div></div>

          <div class="editor-section-title"><span>03</span><h3>{{ purposeOf(draft.entry) === 'chat' ? '能力与验证' : '图片质量' }}</h3></div>
          <template v-if="purposeOf(draft.entry) === 'chat'">
            <div class="field-grid"><div class="field"><label for="context-window">上下文长度</label><input id="context-window" v-model.number="draft.entry.context_window" type="number" min="1" :aria-invalid="!!errors.context_window" :aria-describedby="errors.context_window ? 'error-context_window' : undefined" placeholder="未知时留空" /><small v-if="errors.context_window" id="error-context_window" class="field-error">{{ errors.context_window }}</small></div><div class="field"><label for="max-output">最大输出 Token</label><input id="max-output" v-model.number="draft.entry.max_output_tokens" type="number" min="1" :aria-invalid="!!errors.max_output_tokens" :aria-describedby="errors.max_output_tokens ? 'error-max_output_tokens' : undefined" placeholder="未知时留空" /><small v-if="errors.max_output_tokens" id="error-max_output_tokens" class="field-error">{{ errors.max_output_tokens }}</small></div></div>
            <div class="capability-options"><label><input v-model="draft.entry.capabilities.tools" type="checkbox" />工具调用</label><label><input v-model="draft.entry.capabilities.vision" type="checkbox" />图片输入</label><label><input v-model="draft.entry.capabilities.reasoning" type="checkbox" />推理能力</label></div>
            <label class="verification-check"><input v-model="draft.entry.agent_verified" type="checkbox" :disabled="draftRouteChanged || !draft.entry.capabilities.tools" /><span>已验证工具调用与流式回复<small>{{ draftRouteChanged ? '调用配置已变更，保存后将恢复为待验证。' : '仅在该分组、模型与协议已经过实际验证后勾选。' }}</small></span></label>
          </template>
          <template v-else><p class="field-hint">选择接口支持的质量档位；未选择时使用模型默认质量。</p><div class="quality-options"><label v-for="(label, key) in qualityLabels" :key="key"><input v-model="draft.entry.capabilities.image_quality_options" type="checkbox" :value="key" />{{ label }}</label></div></template>
          <div class="field order-field"><label for="sort-order">显示顺序</label><input id="sort-order" v-model.number="draft.entry.sort_order" type="number" min="0" /><small>数值越小，显示越靠前。</small></div>
        </div>
        <footer class="panel-footer"><span>修改先加入本地草稿</span><button type="button" class="btn btn-secondary" @click="closeEditor">取消编辑</button><button class="btn btn-primary" type="submit" data-testid="save-model">保存模型</button></footer>
      </form>
    </template>
  </PreviewOverlay>

  <PreviewOverlay :open="importOpen" label="从分组导入模型" variant="drawer" @close="importOpen = false">
    <header class="panel-header"><div><span class="panel-eyebrow">批量添加</span><h2>从分组导入模型</h2><p>复用现有分组配置，选择要加入目录的模型。</p></div><button class="icon-button" aria-label="关闭模型导入" @click="importOpen = false"><Icon name="x" /></button></header>
    <div class="panel-scroll import-content"><div class="import-step"><span class="step-number">1</span><h3>选择来源分组</h3></div><div class="field"><label for="import-group">来源分组</label><select id="import-group" v-model.number="importGroup" @change="importFetched = false; importSelection = new Set()"><option v-for="group in availableGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select><small>使用本地候选目录演示，不访问上游接口。</small></div><button class="btn btn-secondary" data-testid="import-fetch" @click="fetchCandidates"><Icon name="sync" />查看候选模型</button>
      <template v-if="importFetched"><div class="import-step import-step-second"><span class="step-number">2</span><h3>选择要导入的模型</h3><span>{{ importCandidates.length }} 个候选</span></div><div class="import-hint"><Icon name="lightbulb" />重复模型会跳过，新增文本模型默认为待验证。</div><label v-for="model in importCandidates" :key="model.id" class="import-model" :class="{ 'already-added': alreadyExists(model) }"><input type="checkbox" :checked="importSelection.has(model.id)" :disabled="alreadyExists(model)" @change="toggleImport(model.id)" /><span><strong>{{ model.display_name }}</strong><code>{{ protocolLabels[model.api_mode] }}</code></span><span class="import-model-status">{{ alreadyExists(model) ? '已在目录中' : '待导入' }}</span></label><p v-if="importCandidates.every(alreadyExists)" class="field-hint">当前分组的候选模型已全部加入，可切换分组继续查看。</p></template>
      <div v-else class="import-empty"><Icon name="grid" /><p>选择分组后查看可导入的模型。</p><small>不会改变上游账号、价格和权限。</small></div>
    </div><footer class="panel-footer"><span>已选择 {{ importSelection.size }} 个</span><button class="btn btn-secondary" @click="importOpen = false">取消导入</button><button class="btn btn-primary" :disabled="!importSelection.size || settings.models.length + importSelection.size > 200" data-testid="import-confirm" @click="confirmImport">导入 {{ importSelection.size }} 个模型</button></footer>
  </PreviewOverlay>

  <PreviewOverlay :open="connectionOpen" label="接入设置" @close="connectionOpen = false"><header class="panel-header"><div><span class="panel-eyebrow">Aino 桌面端</span><h2>接入设置</h2><p>管理桌面端临时调用凭据的有效期。</p></div><button class="icon-button" aria-label="关闭接入设置" @click="connectionOpen = false"><Icon name="x" /></button></header><form class="settings-form" @submit.prevent="applyConnection"><div class="field"><label for="credential-ttl">调用凭据有效期</label><div class="unit-input"><input id="credential-ttl" v-model.number="connectionTTL" type="number" min="300" max="3600" step="1" required /><span>秒</span></div><small>允许 300–3600 秒。当前设置为 {{ Math.round(settings.credential_ttl_seconds / 60) }} 分钟。</small></div><p class="permission-note"><Icon name="shield" />桌面端用户仍需拥有对应分组或订阅权限。</p><footer class="dialog-actions"><button type="button" class="btn btn-secondary" @click="connectionOpen = false">取消设置</button><button class="btn btn-primary" type="submit">保存设置</button></footer></form></PreviewOverlay>

  <PreviewOverlay :open="desktopPreviewOpen" label="Aino 桌面端模型预览" variant="drawer" @close="desktopPreviewOpen = false"><header class="panel-header"><div><span class="panel-eyebrow">用户视角</span><h2>桌面端模型预览</h2><p>查看当前目录在 Aino 中的选择效果。</p></div><button class="icon-button" aria-label="关闭桌面端预览" @click="desktopPreviewOpen = false"><Icon name="x" /></button></header><div class="panel-scroll desktop-preview-content"><div class="desktop-window"><div class="desktop-window-title"><span class="window-dots"><i /><i /><i /></span><img src="/aino-app-icon.png" alt="" width="20" height="20" />Aino</div><div class="desktop-empty-conversation"><img src="/aino-app-icon.png" alt="Aino" width="52" height="52" /><h3>今天想完成什么？</h3><p>选择模型，开始新的对话。</p></div><div class="desktop-composer"><div>向 Aino 发送消息…</div><div><button class="desktop-model-trigger" :aria-expanded="previewPickerOpen" aria-controls="desktop-model-picker" @click="previewPickerOpen = !previewPickerOpen"><Icon name="grid" />{{ selectedPreviewName }}<Icon name="chevronDown" /></button><span class="desktop-send"><Icon name="arrowUp" /></span></div></div><div v-if="previewPickerOpen" id="desktop-model-picker" class="desktop-model-menu"><div class="desktop-menu-head"><span>Aino 内置模型</span><span>{{ previewModels.length }} 个可选</span></div><p v-if="!settings.enabled" class="desktop-menu-empty">当前模型目录已关闭。</p><button v-for="model in previewModels" v-else :key="model.id" class="desktop-model-option" @click="previewSelectedID = model.id; previewPickerOpen = false"><img :src="providerIcon(model)" alt="" width="22" height="22" loading="lazy" decoding="async" /><span>{{ model.display_name }}<small>{{ model.id === currentDefault ? '默认模型' : model.provider_label }}</small></span><Icon v-if="(previewSelectedID || currentDefault) === model.id" name="check" /></button></div></div><div class="preview-explanation"><Icon name="eye" /><p>此处展示本地目录的选择交互。正式桌面端还会根据用户分组权限、余额及模型可用性筛选。</p></div></div></PreviewOverlay>

  <PreviewOverlay :open="removeIDs.length > 0" label="移出模型目录" @close="removeIDs = []"><header class="panel-header"><div><span class="panel-eyebrow">目录管理</span><h2>移出{{ removeIDs.length > 1 ? ` ${removeIDs.length} 个` : '' }}模型？</h2><p>{{ removeIDs.length === 1 ? settings.models.find(model => model.id === removeIDs[0])?.display_name : '所选模型' }}将从当前内置目录中移除。</p></div><button class="icon-button" aria-label="取消移除" @click="removeIDs = []"><Icon name="x" /></button></header><div class="confirmation-body"><p>上游账号和分组会保留。若包含默认模型，对应的默认选择将清空。</p><footer class="dialog-actions"><button class="btn btn-secondary" @click="removeIDs = []">取消移除</button><button class="btn btn-danger" data-testid="remove-confirm" @click="removeModels">确认移出目录</button></footer></div></PreviewOverlay>

  <PreviewOverlay :open="discardOpen" label="放弃配置更改" @close="discardOpen = false"><header class="panel-header"><div><h2>放弃配置更改？</h2><p>模型目录将恢复到上次本地保存的状态。</p></div><button class="icon-button" aria-label="取消放弃更改" @click="discardOpen = false"><Icon name="x" /></button></header><div class="confirmation-body"><footer class="dialog-actions"><button class="btn btn-secondary" @click="discardOpen = false">继续编辑</button><button class="btn btn-primary" @click="settings = clone(saved); selected = new Set(); discardOpen = false">放弃更改</button></footer></div></PreviewOverlay>

  <PreviewOverlay :open="editorDiscardOpen" label="放弃模型编辑" @close="editorDiscardOpen = false"><header class="panel-header"><div><h2>放弃当前模型编辑？</h2><p>尚未保存到草稿的字段修改将被放弃。</p></div></header><div class="confirmation-body"><footer class="dialog-actions"><button class="btn btn-secondary" @click="editorDiscardOpen = false">继续编辑</button><button class="btn btn-primary" @click="editorDiscardOpen = false; draft = null">放弃模型编辑</button></footer></div></PreviewOverlay>

  <PreviewOverlay :open="helpOpen" label="模型目录配置说明" @close="helpOpen = false"><header class="panel-header"><div><h2>模型目录配置说明</h2><p>管理目录与实际调用使用同一套模型配置。</p></div><button class="icon-button" aria-label="关闭配置说明" @click="helpOpen = false"><Icon name="x" /></button></header><div class="help-content"><div><h3>文本与生图分开</h3><p>两类目录各自设置默认模型。生图固定使用 Images API，不参与文本默认模型选择。</p></div><div><h3>验证后设置默认</h3><p>文本模型支持工具调用，且已验证 Agent 工具与流式回复后，才能设为默认。更换分组、模型或协议会重置验证状态。</p></div><div><h3>沿用已有计费</h3><p>目录不另设价格。权限、用户倍率和收费规则沿用已有分组及渠道配置。</p></div><div class="help-preview"><strong>当前为本地交互预览</strong><p>不读取真实账号、不发起模型请求。保存仅写入当前浏览器；原始预览数据可随时恢复。</p><button class="btn btn-secondary" @click="resetPreview">恢复初始预览</button></div></div></PreviewOverlay>

  <Transition name="toast"><div v-if="toastMessage" class="toast-message" role="status"><Icon name="checkCircle" />{{ toastMessage }}</div></Transition>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { DesktopModelEntry, DesktopSettings } from '@/api/admin/settings'
import PreviewOverlay from './PreviewOverlay.vue'
import openaiIcon from './assets/openai.svg'
import { canDefault, candidates, clone, groups, initialSettings, normalizeDefaults, protocolLabels, purposeOf, replaceEntry, validateEntry, type Purpose } from './catalog'

const storageKey = 'omniflow:desktop-models-design:v1'
function loadSettings(): DesktopSettings {
  try {
    const raw = localStorage.getItem(storageKey)
    if (raw) { const value = JSON.parse(raw); if (Array.isArray(value.models) && typeof value.enabled === 'boolean' && value.models.length <= 200) return value }
  } catch { /* A private browser can deny storage; the in-memory preview still works. */ }
  return initialSettings()
}
const saved = ref<DesktopSettings>(loadSettings())
const settings = ref<DesktopSettings>(clone(saved.value))
const dirty = computed(() => JSON.stringify(settings.value) !== JSON.stringify(saved.value))
const savedMessage = ref('')
const purpose = ref<Purpose>('chat')
const tabs = [{ purpose: 'chat', label: '文本模型', icon: 'chat' }, { purpose: 'image', label: '生图模型', icon: 'grid' }] as const
const nav: Array<{ label: string; icon: InstanceType<typeof Icon>['$props']['name']; active?: boolean }> = [
  { label: '仪表盘', icon: 'chart' }, { label: '运维监控', icon: 'radar' },
  { label: '用户管理', icon: 'users' }, { label: '分组管理', icon: 'document' },
  { label: '账号管理', icon: 'server' }, { label: '渠道管理', icon: 'arrowsUpDown' },
  { label: '内置模型', icon: 'grid', active: true }, { label: '系统设置', icon: 'cog' },
]
const search = ref('')
const searchInput = ref<HTMLInputElement>()
const groupFilter = ref('all')
const statusFilter = ref('all')
const selected = ref(new Set<string>())
const sidebarOpen = ref(false)
const activeModels = computed(() => settings.value.models.filter(model => purposeOf(model) === purpose.value).sort((a, b) => a.sort_order - b.sort_order))
const availableGroups = computed(() => groups.filter(group => purpose.value === 'chat' ? group.id !== 5 : group.platform === 'openai'))
const filteredModels = computed(() => activeModels.value.filter(model => {
  const query = search.value.trim().toLowerCase()
  return (!query || [model.display_name, model.id, model.model, model.provider_label, groupName(model.group_id)].some(value => value.toLowerCase().includes(query)))
    && (groupFilter.value === 'all' || model.group_id === Number(groupFilter.value))
    && (purpose.value === 'image' || statusFilter.value === 'all' || (model.agent_verified ? 'verified' : 'pending') === statusFilter.value)
}))
const defaultOptions = computed(() => activeModels.value.filter(canDefault))
const currentDefault = computed({
  get: () => (purpose.value === 'chat' ? settings.value.default_model_id : settings.value.default_image_model_id) || '',
  set: value => {
    if (purpose.value === 'chat') settings.value.default_model_id = value || null
    else settings.value.default_image_model_id = value || null
  },
})
function groupName(id: number): string { return groups.find(group => group.id === id)?.name || `分组 #${id}` }
function providerKey(model: DesktopModelEntry): string {
  return model.platform === 'anthropic' ? 'claude' : ['deepseek', 'gemini'].includes(model.platform) ? model.platform : 'openai'
}
function providerIcon(model: DesktopModelEntry): string {
  const key = providerKey(model)
  return key === 'openai' ? openaiIcon : `/omniflow-home/${key}.svg`
}
function resetFilters(): void { search.value = ''; groupFilter.value = 'all'; statusFilter.value = 'all'; selected.value = new Set() }
async function changePurpose(value: Purpose, focus = false): Promise<void> {
  purpose.value = value; previewSelectedID.value = ''; resetFilters()
  if (focus) { await nextTick(); document.getElementById(`tab-${value}`)?.focus() }
}
const allSelected = computed(() => filteredModels.value.length > 0 && filteredModels.value.every(model => selected.value.has(model.id)))
function toggleSelected(id: string): void { const next = new Set(selected.value); if (next.has(id)) next.delete(id); else next.add(id); selected.value = next }
function toggleAll(): void { selected.value = allSelected.value ? new Set() : new Set(filteredModels.value.map(model => model.id)) }
watch([search, groupFilter, statusFilter], () => { selected.value = new Set() })
function setDefault(model: DesktopModelEntry): void { if (canDefault(model)) { currentDefault.value = model.id; notify(`已将 ${model.display_name} 设为默认，保存配置后生效。`) } }

const draft = ref<{ previousID: string | null; entry: DesktopModelEntry } | null>(null)
const draftBaseline = ref('')
const editorDiscardOpen = ref(false)
const errors = ref<Record<string, string>>({})
const editorGroups = computed(() => groups.filter(group => !draft.value || purposeOf(draft.value.entry) === 'chat' ? group.id !== 5 : group.platform === 'openai'))
const editorPlatforms = computed(() => draft.value && purposeOf(draft.value.entry) === 'image' ? ['openai', 'grok'] : ['openai', 'anthropic', 'gemini', 'deepseek', 'grok', 'kimi', 'zhipu', 'minimax', 'antigravity', 'opencode_go'])
const editorProtocols = computed<DesktopModelEntry['api_mode'][]>(() => draft.value && purposeOf(draft.value.entry) === 'image' ? ['images_generations'] : ['chat_completions', 'responses', 'anthropic_messages'])
const draftRouteChanged = computed(() => {
  if (!draft.value?.previousID) return false
  const previous = settings.value.models.find(model => model.id === draft.value!.previousID)
  return !!previous && (['model', 'group_id', 'platform', 'api_mode'] as const).some(key => previous[key] !== draft.value!.entry[key])
})
function openEditor(model?: DesktopModelEntry): void {
  errors.value = {}
  const image = purpose.value === 'image'
  const next: DesktopModelEntry = model ? clone(model) : {
    id: '', display_name: '', model: '', provider_label: '', group_id: 0, platform: 'openai',
    api_mode: image ? 'images_generations' : 'chat_completions', sort_order: settings.value.models.length,
    agent_verified: false, context_window: null, max_output_tokens: null,
    capabilities: { tools: false, vision: false, reasoning: false, ...(image ? { image_generation: true, image_quality_options: [] } : {}) },
  }
  draft.value = { previousID: model?.id || null, entry: next }; draftBaseline.value = JSON.stringify(next)
}
function closeEditor(): void { if (draft.value && JSON.stringify(draft.value.entry) !== draftBaseline.value) editorDiscardOpen.value = true; else draft.value = null }
function setDraftGroup(event: Event): void {
  if (!draft.value) return
  const group = groups.find(item => item.id === Number((event.target as HTMLSelectElement).value))
  if (!group) return
  draft.value.entry.group_id = group.id; draft.value.entry.platform = group.platform
}
async function saveEntry(): Promise<void> {
  if (!draft.value) return
  const entry = clone(draft.value.entry)
  for (const key of ['context_window', 'max_output_tokens'] as const) if (typeof entry[key] !== 'number') entry[key] = null
  errors.value = validateEntry(entry, settings.value.models, draft.value.previousID)
  if (Object.keys(errors.value).length) { await nextTick(); document.querySelector<HTMLInputElement>('.editor-form [aria-invalid="true"]')?.focus(); return }
  if (!draft.value.previousID && settings.value.models.length >= 200) { notify('模型目录最多允许 200 个模型。'); return }
  settings.value = replaceEntry(settings.value, draft.value.previousID, entry)
  draft.value = null; notify('模型已加入本地草稿。')
}

const qualityLabels: Record<string, string> = { auto: '自动', low: '低', medium: '中', high: '高', xhigh: '超高', max: '最高' }
const importOpen = ref(false)
const importGroup = ref(2)
const importFetched = ref(false)
const importSelection = ref(new Set<string>())
const importCandidates = computed(() => candidates(purpose.value, importGroup.value))
function openImporter(): void { importGroup.value = purpose.value === 'chat' ? 2 : 5; importFetched.value = false; importSelection.value = new Set(); importOpen.value = true }
function alreadyExists(model: DesktopModelEntry): boolean { return settings.value.models.some(item => item.model === model.model && item.group_id === model.group_id && item.api_mode === model.api_mode) }
function fetchCandidates(): void { importFetched.value = true; importSelection.value = new Set(importCandidates.value.filter(model => !alreadyExists(model)).map(model => model.id)) }
function toggleImport(id: string): void { const next = new Set(importSelection.value); if (next.has(id)) next.delete(id); else next.add(id); importSelection.value = next }
function confirmImport(): void {
  const additions = importCandidates.value.filter(model => importSelection.value.has(model.id) && !alreadyExists(model))
  if (settings.value.models.length + additions.length > 200) return
  additions.forEach((model, index) => {
    let id = model.id; let suffix = 2
    while (settings.value.models.some(item => item.id === id)) id = `${model.id}-${suffix++}`
    settings.value.models.push({ ...clone(model), id, sort_order: settings.value.models.length + index })
  })
  importOpen.value = false; resetFilters(); notify(`已将 ${additions.length} 个模型加入本地草稿。`)
}

const removeIDs = ref<string[]>([])
function requestRemove(ids: string[]): void { removeIDs.value = ids }
function removeModels(): void {
  const removed = new Set(removeIDs.value)
  settings.value = normalizeDefaults({ ...settings.value, models: settings.value.models.filter(model => !removed.has(model.id)) })
  selected.value = new Set(); removeIDs.value = []; notify('模型已从本地草稿移出。')
}
const discardOpen = ref(false)
function saveSettings(): void {
  try {
    const next = normalizeDefaults(settings.value)
    localStorage.setItem(storageKey, JSON.stringify(next)); saved.value = clone(next); settings.value = clone(next)
    notify('配置已保存到当前浏览器。')
  } catch { notify('浏览器未允许本地保存，草稿已保留。') }
}
function resetPreview(): void {
  try { localStorage.removeItem(storageKey) } catch { /* Memory reset remains available. */ }
  settings.value = initialSettings(); saved.value = clone(settings.value); resetFilters(); helpOpen.value = false; notify('已恢复初始预览数据。')
}

const connectionOpen = ref(false)
const connectionTTL = ref(settings.value.credential_ttl_seconds)
watch(connectionOpen, open => { if (open) connectionTTL.value = settings.value.credential_ttl_seconds })
function applyConnection(): void {
  if (!Number.isInteger(connectionTTL.value) || connectionTTL.value < 300 || connectionTTL.value > 3600) return
  settings.value.credential_ttl_seconds = connectionTTL.value; connectionOpen.value = false; notify('接入设置已更新到本地草稿。')
}
const desktopPreviewOpen = ref(false)
const previewPickerOpen = ref(true)
watch(desktopPreviewOpen, open => { if (open) previewPickerOpen.value = true })
const previewSelectedID = ref('')
const previewModels = computed(() => settings.value.enabled ? activeModels.value.filter(canDefault) : [])
const selectedPreviewName = computed(() => previewModels.value.find(model => model.id === (previewSelectedID.value || currentDefault.value))?.display_name || '选择模型')
const helpOpen = ref(false)
const toastMessage = ref('')
let toastTimer: ReturnType<typeof setTimeout> | undefined
function notify(message: string): void { clearTimeout(toastTimer); toastMessage.value = message; toastTimer = setTimeout(() => { toastMessage.value = '' }, 3000) }
function shortcuts(event: KeyboardEvent): void {
  if (event.key !== '/' || (event.target instanceof HTMLElement && ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target.tagName)) || document.querySelector('dialog[open]')) return
  event.preventDefault(); searchInput.value?.focus()
}
function beforeUnload(event: BeforeUnloadEvent): void { if (dirty.value || draft.value && JSON.stringify(draft.value.entry) !== draftBaseline.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => {
  document.addEventListener('keydown', shortcuts); window.addEventListener('beforeunload', beforeUnload)
  // Shareable, read-only entry points to the actual prototype panels.
  const query = new URLSearchParams(window.location.search)
  const panel = query.get('panel')
  if (panel === 'editor') openEditor(settings.value.models.find(model => model.id === query.get('model')))
  else if (panel === 'import') openImporter()
  else if (panel === 'desktop') desktopPreviewOpen.value = true
  else if (panel === 'connection') connectionOpen.value = true
})
onBeforeUnmount(() => { clearTimeout(toastTimer); document.removeEventListener('keydown', shortcuts); window.removeEventListener('beforeunload', beforeUnload) })
</script>
