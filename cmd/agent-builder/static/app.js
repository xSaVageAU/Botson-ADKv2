let allAgents = [];
let allTools = { standard: [], agents: [] };
let currentAgent = null;

// Load initial data
window.addEventListener('DOMContentLoaded', async () => {
  await loadTools();
  await loadAgents();
});

async function loadAgents() {
  try {
    const res = await fetch('/api/agents');
    if (!res.ok) throw new Error('Failed to load agents list');
    allAgents = await res.json();
    renderSidebar();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function loadTools() {
  try {
    const res = await fetch('/api/tools');
    if (!res.ok) throw new Error('Failed to load tools list');
    allTools = await res.json();
  } catch (err) {
    showToast(err.message, 'error');
  }
}

function renderSidebar() {
  const listEl = document.getElementById('agentList');
  listEl.innerHTML = '';

  allAgents.forEach(agent => {
    const li = document.createElement('li');
    li.className = `agent-item ${currentAgent && currentAgent.name === agent.name ? 'active' : ''}`;
    li.onclick = () => selectAgent(agent);

    li.innerHTML = `
      <div class="agent-info">
        <div class="agent-name">${agent.name}</div>
        <div class="agent-desc-short">${agent.description || 'No description'}</div>
      </div>
      <div style="display: flex; align-items: center; gap: 8px;">
        <span class="badge ${agent.read_only ? 'read-only' : ''}">${agent.read_only ? 'Default' : 'Custom'}</span>
        ${!agent.read_only ? `
          <button class="btn-delete" onclick="deleteAgent(event, '${agent.name}')">Delete</button>
        ` : ''}
      </div>
    `;
    listEl.appendChild(li);
  });
}

function selectAgent(agent) {
  currentAgent = agent;
  renderSidebar();
  renderForm(agent);
}

function openNewAgentForm() {
  currentAgent = null;
  renderSidebar();
  renderForm({
    name: '',
    description: '',
    is_root: false,
    private: false,
    tools: [],
    instructions: '',
    read_only: false
  });
}

function renderForm(agentData) {
  const isEdit = agentData.name !== '';
  const workspace = document.getElementById('workspace');

  // Create checkbox lists for tools
  const filteredSubagents = allTools.agents.filter(a => a !== agentData.name);

  let toolsHTML = '';
  
  // Standard Tools Section
  toolsHTML += `
    <fieldset>
      <legend>Standard Tools</legend>
      <div class="grid" style="margin-bottom: 0;">
        ${allTools.standard.map(tool => {
          const checked = agentData.tools && agentData.tools.includes(tool) ? 'checked' : '';
          return `
            <label style="margin-bottom: 0;">
              <input type="checkbox" name="agent_tools" value="${tool}" ${checked}>
              ${tool}
            </label>
          `;
        }).join('')}
      </div>
    </fieldset>
  `;

  // Sub-Agents delegation Section
  if (filteredSubagents.length > 0) {
    toolsHTML += `
      <fieldset>
        <legend>Sub-Agent Delegation</legend>
        <div class="grid" style="margin-bottom: 0;">
          ${filteredSubagents.map(sub => {
            const checked = agentData.tools && agentData.tools.includes(sub) ? 'checked' : '';
            return `
              <label style="margin-bottom: 0;">
                <input type="checkbox" name="agent_tools" value="${sub}" ${checked}>
                ${sub}
              </label>
            `;
          }).join('')}
        </div>
      </fieldset>
    `;
  }

  workspace.innerHTML = `
    <article style="max-width: 800px; margin: 0 auto; padding: 24px;">
      <header style="display: flex; justify-content: space-between; align-items: center; padding-bottom: 12px; margin-bottom: 20px;">
        <h3 style="margin: 0; font-size: 1.25rem;">${isEdit ? `Edit Agent: ${agentData.name}` : 'Create New Agent'}</h3>
        <span class="badge ${agentData.read_only ? 'read-only' : ''}">${agentData.read_only ? 'Default Agent' : 'Custom Agent'}</span>
      </header>

      ${agentData.read_only ? `
        <div class="read-only-alert" style="margin-bottom: 20px; background-color: var(--pico-form-element-invalid-focus-color); border: 1px solid var(--pico-form-element-invalid-border-color); padding: 12px; border-radius: var(--pico-border-radius); font-size: 0.85rem;">
          <strong>Notice:</strong> This is a compiled-in default agent. Saving edits will create a custom override in your home directory.
        </div>
      ` : ''}

      <div class="grid">
        <label>
          Agent Name (Unique ID)
          <input type="text" id="agentName" value="${agentData.name}" placeholder="e.g. general_assistant" ${isEdit ? 'disabled' : ''}>
        </label>
        <label>
          Description
          <input type="text" id="agentDesc" value="${agentData.description}" placeholder="e.g. A general helper agent">
        </label>
      </div>

      <fieldset style="display: flex; gap: 40px; margin-bottom: 20px; border: 1px solid var(--pico-border-color); padding: 16px; border-radius: var(--pico-border-radius);">
        <legend style="padding: 0 8px; font-size: 0.8rem; font-weight: 600;">Configurations</legend>
        <label style="margin-bottom: 0;">
          <input type="checkbox" id="agentIsRoot" role="switch" ${agentData.is_root ? 'checked' : ''}>
          Is Root Agent
        </label>
        <label style="margin-bottom: 0;">
          <input type="checkbox" id="agentIsPrivate" role="switch" ${agentData.private ? 'checked' : ''}>
          Private (Hide from selector)
        </label>
      </fieldset>

      ${toolsHTML}

      <label>
        Instructions (System Prompt - Markdown)
        <textarea id="agentInstructions" placeholder="You are a polite assistant..." style="min-height: 150px; font-family: monospace; font-size: 0.9rem;">${agentData.instructions || ''}</textarea>
      </label>

      <footer style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 20px; padding-top: 16px;">
        <button class="secondary" onclick="openNewAgentForm()" style="width: auto; margin-bottom: 0;">Cancel</button>
        <button onclick="saveAgent()" style="width: auto; margin-bottom: 0;">Save Agent</button>
      </footer>
    </article>
  `;
}

async function saveAgent() {
  const nameEl = document.getElementById('agentName');
  const descEl = document.getElementById('agentDesc');
  const isRootEl = document.getElementById('agentIsRoot');
  const isPrivateEl = document.getElementById('agentIsPrivate');
  const instEl = document.getElementById('agentInstructions');
  
  const selectedTools = [];
  document.querySelectorAll('input[name="agent_tools"]:checked').forEach(cb => {
    selectedTools.push(cb.value);
  });

  const payload = {
    name: nameEl.value,
    description: descEl.value,
    is_root: isRootEl.checked,
    private: isPrivateEl.checked,
    tools: selectedTools,
    instructions: instEl.value
  };

  try {
    const res = await fetch('/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to save agent');
    }

    showToast('Agent saved successfully!', 'success');
    
    await loadTools();
    await loadAgents();

    const matched = allAgents.find(a => a.name === payload.name);
    if (matched) selectAgent(matched);

  } catch (err) {
    showToast(err.message, 'error');
  }
}

async function deleteAgent(event, name) {
  event.stopPropagation();

  if (!confirm(`Are you sure you want to delete "${name}"? This cannot be undone.`)) {
    return;
  }

  try {
    const res = await fetch(`/api/agents/${name}`, {
      method: 'DELETE'
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to delete agent');
    }

    showToast('Agent deleted successfully', 'success');
    
    if (currentAgent && currentAgent.name === name) {
      currentAgent = null;
      document.getElementById('workspace').innerHTML = `
        <div class="welcome-container">
          <div class="welcome-card" style="text-align: center;">
            <h2>Agent Builder Dashboard</h2>
            <p>Create and orchestrate agents. Add custom prompts, link tools, and wire up sub-agent delegation. Saving changes automatically updates your local <code>~/.botsonv2/agents/</code> directory.</p>
          </div>
        </div>
      `;
    }

    await loadTools();
    await loadAgents();

  } catch (err) {
    showToast(err.message, 'error');
  }
}

function showToast(message, type = 'success') {
  const container = document.getElementById('toastContainer');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.innerText = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.remove();
  }, 3000);
}
