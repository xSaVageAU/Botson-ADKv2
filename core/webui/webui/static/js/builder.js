// Agent Builder Config View Controller

window.loadAgentsForBuilder = async function() {
  try {
    const res = await fetch('/botson/api/agents');
    if (!res.ok) throw new Error('Failed to load agents list');
    window.allAgents = await res.json();
    window.renderBuilderSidebar();
  } catch (err) {
    window.showToast(err.message, 'error');
  }
};

window.loadToolsForBuilder = async function() {
  try {
    const res = await fetch('/botson/api/tools');
    if (!res.ok) throw new Error('Failed to load tools list');
    window.allTools = await res.json();
  } catch (err) {
    window.showToast(err.message, 'error');
  }
};

window.renderBuilderSidebar = function() {
  const listEl = document.getElementById('agentList');
  const countEl = document.getElementById('agentCount');
  if (!listEl) return;
  listEl.innerHTML = '';
  if (countEl) countEl.textContent = window.allAgents.length;

  window.allAgents.forEach(agent => {
    const li = document.createElement('li');
    li.className = `node-row ${window.currentAgent && window.currentAgent.name === agent.name ? 'active' : ''}`;
    li.onclick = () => window.selectAgent(agent);

    const glyphClasses = [
      'node-glyph',
      agent.is_root ? 'is-root' : '',
      agent.private ? 'is-private' : ''
    ].filter(Boolean).join(' ');

    li.innerHTML = `
      <div class="node-row-top">
        <span class="${glyphClasses}" title="${agent.is_root ? 'root agent' : agent.private ? 'private' : ''}"></span>
        <span class="node-name">${window.escapeHtml(agent.name)}</span>
        <span class="node-tag ${agent.read_only ? '' : 'tag-custom'}">${agent.read_only ? 'default' : 'custom'}</span>
      </div>
      <div class="node-row-bottom">
        <div class="node-desc">${window.escapeHtml(agent.description || 'No description')}</div>
        ${!agent.read_only ? `<button class="btn-delete" onclick="window.deleteAgent(event, '${agent.name}')">Delete</button>` : ''}
      </div>
    `;
    listEl.appendChild(li);
  });
};

window.selectAgent = function(agent) {
  window.currentAgent = agent;
  window.renderBuilderSidebar();
  
  const welcome = document.getElementById('builder-welcome');
  if (welcome) welcome.style.display = 'none';
  const editor = document.getElementById('builder-editor');
  if (editor) {
    editor.style.display = 'block';
    window.renderForm(agent);
  }
};

window.openNewAgentForm = function() {
  window.currentAgent = null;
  window.renderBuilderSidebar();
  
  const welcome = document.getElementById('builder-welcome');
  if (welcome) welcome.style.display = 'none';
  const editor = document.getElementById('builder-editor');
  if (editor) {
    editor.style.display = 'block';
    window.renderForm({
      name: '',
      description: '',
      is_root: false,
      private: false,
      tools: [],
      instructions: '',
      read_only: false
    });
  }
};

window.renderForm = function(agentData) {
  const isEdit = agentData.name !== '';
  const editor = document.getElementById('builder-editor');
  if (!editor) return;

  const filteredSubagents = window.allTools.agents.filter(a => a !== agentData.name);
  const toolCount = (agentData.tools || []).filter(t => window.allTools.standard.includes(t)).length;
  const subCount = (agentData.tools || []).filter(t => filteredSubagents.includes(t)).length;

  let groupsHTML = '';

  groupsHTML += `
    <div class="group">
      <span class="group-title">Standard tools</span>
      <div class="tools-container">
        ${window.allTools.standard.map(tool => {
          const checked = agentData.tools && agentData.tools.includes(tool) ? 'checked' : '';
          return `
            <label class="tool-item">
              <input type="checkbox" name="tools" value="${window.escapeHtml(tool)}" ${checked} onchange="window.updateWiringStrip()">
              <span>${window.escapeHtml(tool)}</span>
            </label>
          `;
        }).join('')}
      </div>
    </div>
  `;

  if (filteredSubagents.length > 0) {
    groupsHTML += `
      <div class="group">
        <span class="group-title">Sub-agent delegation</span>
        <div class="tools-container">
          ${filteredSubagents.map(subName => {
            const checked = agentData.tools && agentData.tools.includes(subName) ? 'checked' : '';
            return `
              <label class="tool-item">
                <input type="checkbox" name="tools" value="${window.escapeHtml(subName)}" ${checked} onchange="window.updateWiringStrip()">
                <span>${window.escapeHtml(subName)}</span>
              </label>
            `;
          }).join('')}
        </div>
      </div>
    `;
  }

  editor.innerHTML = `
    <header class="editor-header">
      <div>
        <span class="eyebrow">${agentData.read_only ? 'Default Agent (Read-only)' : isEdit ? 'Modify Agent' : 'Create Agent'}</span>
        <h3 id="headingName">${window.escapeHtml(agentData.name || 'new_agent')}</h3>
      </div>
      <span class="pill ${agentData.read_only ? '' : 'pill-accent'}">${agentData.read_only ? 'system' : 'custom'}</span>
    </header>

    ${agentData.read_only ? `
      <div class="notice">
        <strong>⚠️ Read-Only Default:</strong> This agent is bundled with the application. You cannot modify its settings directly. However, saving changes will automatically clone it into a custom editable agent.
      </div>
    ` : ''}

    <!-- Wiring schematic -->
    <div class="wiring-strip">
      <div class="wire-node">
        <span class="wire-dot"></span>
        <span class="wire-name-label" id="stripName">${window.escapeHtml(agentData.name || 'new_agent')}</span>
      </div>
      <div class="wire-trace"></div>
      <div class="wire-node">
        <span class="wire-count" id="stripToolCount">${toolCount}</span>
        <span class="wire-label">Tools</span>
      </div>
      <div class="wire-trace"></div>
      <div class="wire-node">
        <span class="wire-count" id="stripSubCount">${subCount}</span>
        <span class="wire-label">Subagents</span>
      </div>
      <div class="wire-trace"></div>
      <div class="wire-node">
        <span class="wire-dot dot-teal"></span>
        <span class="wire-label">Execute</span>
      </div>
    </div>

    <!-- Inputs -->
    <div class="field-row">
      <div class="field">
        <label for="agentNameInput">Agent Unique Name</label>
        <input type="text" id="agentNameInput" value="${window.escapeHtml(agentData.name)}" ${isEdit ? 'disabled' : ''} placeholder="e.g. general_assistant" oninput="window.handleNameInput(this)">
      </div>
      <div class="field">
        <label for="agentDescInput">Short Description</label>
        <input type="text" id="agentDescInput" value="${window.escapeHtml(agentData.description)}" placeholder="What does this agent specialize in?">
      </div>
    </div>

    <div class="switch-row">
      <label class="builder-switch">
        <input type="checkbox" id="agentRootInput" ${agentData.is_root ? 'checked' : ''}>
        <span>Is Root (Entry point)</span>
      </label>
    </div>

    <div class="field" style="margin-bottom: 16px;">
      <label for="agentPromptInput">System Instructions / Prompt</label>
      <textarea id="agentPromptInput" placeholder="You are a helpful assistant that... Describe the persona, guidelines, and behavioral rules.">${window.escapeHtml(agentData.instructions || '')}</textarea>
    </div>

    ${groupsHTML}

    <div class="editor-actions">
      <button class="btn" onclick="window.cancelEditor()">Cancel</button>
      <button class="btn btn-primary" onclick="window.saveAgent()">Save Configuration</button>
    </div>
  `;
};

window.handleNameInput = function(el) {
  const val = el.value.trim();
  const heading = document.getElementById('headingName');
  const strip = document.getElementById('stripName');
  if (heading) heading.textContent = val || 'new_agent';
  if (strip) strip.textContent = val || 'new_agent';
};

window.updateWiringStrip = function() {
  const checkedCheckboxes = Array.from(document.querySelectorAll('input[name="tools"]:checked')).map(cb => cb.value);
  const toolCount = checkedCheckboxes.filter(t => window.allTools.standard.includes(t)).length;
  const subCount = checkedCheckboxes.filter(t => window.allTools.agents.includes(t)).length;
  
  const stripTool = document.getElementById('stripToolCount');
  const stripSub = document.getElementById('stripSubCount');
  if (stripTool) stripTool.textContent = toolCount;
  if (stripSub) stripSub.textContent = subCount;
};

window.cancelEditor = function() {
  window.currentAgent = null;
  window.renderBuilderSidebar();
  const editor = document.getElementById('builder-editor');
  if (editor) editor.style.display = 'none';
  const welcome = document.getElementById('builder-welcome');
  if (welcome) welcome.style.display = 'block';
};

window.saveAgent = async function() {
  const nameInput = document.getElementById('agentNameInput');
  const descInput = document.getElementById('agentDescInput');
  const isRootInput = document.getElementById('agentRootInput');
  const promptInput = document.getElementById('agentPromptInput');
  
  if (!nameInput) return;
  const name = nameInput.value.trim();
  if (!name) {
    window.showToast('Agent name is required', 'error');
    return;
  }

  const selectedTools = Array.from(document.querySelectorAll('input[name="tools"]:checked')).map(cb => cb.value);

  const payload = {
    name: name,
    description: descInput ? descInput.value.trim() : '',
    instructions: promptInput ? promptInput.value.trim() : '',
    readOnly: false,
    agentConfig: {
      name: name,
      description: descInput ? descInput.value.trim() : '',
      is_root: isRootInput ? isRootInput.checked : false,
      tools: selectedTools,
    }
  };

  try {
    const res = await fetch('/botson/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Failed to save agent');
    }

    window.showToast('Agent configuration saved successfully', 'success');
    await window.loadAgentsForBuilder();
    window.cancelEditor();
  } catch (err) {
    window.showToast(`Save failed: ${err.message}`, 'error');
  }
};

window.deleteAgent = async function(event, agentName) {
  event.stopPropagation();
  if (!confirm(`Are you sure you want to delete agent "${agentName}"?`)) return;

  try {
    const res = await fetch(`/botson/api/agents/${agentName}`, {
      method: 'DELETE'
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Failed to delete agent');
    }

    window.showToast('Agent deleted', 'success');
    if (window.currentAgent && window.currentAgent.name === agentName) {
      window.cancelEditor();
    }
    await window.loadAgentsForBuilder();
  } catch (err) {
    window.showToast(`Delete failed: ${err.message}`, 'error');
  }
};
