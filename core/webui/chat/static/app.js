const API_BASE = 'http://localhost:8080/api';
let activeAgent = null;
let activeSessionId = null;
let activeTab = 'state';

// Initial bootstrap
window.addEventListener('DOMContentLoaded', async () => {
  await loadAgents();
});

// Load registered agents
async function loadAgents() {
  const selector = document.getElementById('agentSelector');
  selector.innerHTML = '<option value="">Loading agents...</option>';

  try {
    const res = await fetch(`${API_BASE}/list-apps`);
    if (!res.ok) throw new Error('Failed to load apps');
    const apps = await res.json(); // Array of strings e.g. ["general_assistant"]

    selector.innerHTML = '';
    if (apps.length === 0) {
      selector.innerHTML = '<option value="">No agents found</option>';
      return;
    }

    apps.forEach(appName => {
      const opt = document.createElement('option');
      opt.value = appName;
      opt.textContent = appName;
      selector.appendChild(opt);
    });

    switchAgent();
  } catch (err) {
    showToast(`API Connection Error: Make sure prod launcher is running on port 8080.`, 'error');
    selector.innerHTML = '<option value="">Connection error</option>';
  }
}

// Switch active agent
async function switchAgent() {
  const selector = document.getElementById('agentSelector');
  activeAgent = selector.value;

  if (!activeAgent) {
    document.getElementById('activeAgentName').textContent = 'No Agent Selected';
    document.getElementById('activeAgentSub').textContent = 'AGENT';
    document.getElementById('sessionPill').textContent = 'no session';
    document.getElementById('sessionPill').className = 'pill';
    document.getElementById('sessionList').innerHTML = '';
    document.getElementById('sessionCount').textContent = '0';
    return;
  }

  document.getElementById('activeAgentName').textContent = activeAgent;
  document.getElementById('activeAgentSub').textContent = 'ACTIVE AGENT';
  
  // Clear chat log
  clearChatLog();
  clearInspector();

  await loadSessions();
}

// Load historical sessions
async function loadSessions() {
  if (!activeAgent) return;
  const listEl = document.getElementById('sessionList');
  listEl.innerHTML = '<li style="padding: 10px; font-size: 12px; color: var(--text-faint);">Loading sessions...</li>';

  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions`);
    if (!res.ok) throw new Error('Failed to load sessions');
    const sessions = await res.json(); // Array of Session objects

    listEl.innerHTML = '';
    document.getElementById('sessionCount').textContent = sessions.length;

    if (sessions.length === 0) {
      listEl.innerHTML = '<li style="padding: 10px; font-size: 12px; color: var(--text-faint); font-style: italic;">No active sessions found</li>';
      // Automatically start a new session if empty
      await startNewSession();
      return;
    }

    // Sort by update time descending
    sessions.sort((a, b) => b.lastUpdateTime - a.lastUpdateTime);

    sessions.forEach(s => {
      const li = document.createElement('li');
      li.className = `session-row ${activeSessionId === s.id ? 'active' : ''}`;
      li.dataset.id = s.id;
      li.onclick = () => selectSession(s.id);

      const timeStr = new Date(s.lastUpdateTime * 1000).toLocaleString();

      let title = s.id;
      if (s.state && s.state.__session_metadata__ && s.state.__session_metadata__.displayName) {
        title = s.state.__session_metadata__.displayName;
      }

      li.innerHTML = `
        <div class="session-info">
          <span class="session-id-text" title="${escapeHtml(s.id)}">${escapeHtml(title)}</span>
          <span class="session-time-text">${timeStr}</span>
        </div>
        <button class="btn-delete-session" onclick="deleteSession(event, '${s.id}')" title="Delete session">🗑</button>
      `;
      listEl.appendChild(li);
    });

    // If no active session is selected, select the most recent one
    if (!activeSessionId && sessions.length > 0) {
      selectSession(sessions[0].id);
    }
  } catch (err) {
    showToast(`Failed to load sessions: ${err.message}`, 'error');
  }
}

// Select session
async function selectSession(sessionId) {
  activeSessionId = sessionId;
  
  // Highlight active session row in list
  document.querySelectorAll('.session-row').forEach(row => {
    row.className = `session-row ${row.dataset.id === sessionId ? 'active' : ''}`;
  });

  document.getElementById('sessionPill').textContent = sessionId;
  document.getElementById('sessionPill').className = 'pill pill-accent';

  clearChatLog();
  clearInspector();

  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${sessionId}`);
    if (!res.ok) throw new Error('Failed to load session details');
    const sessionData = await res.json();

    // Set Header Title to displayName or ID
    let title = sessionData.id;
    if (sessionData.state && sessionData.state.__session_metadata__ && sessionData.state.__session_metadata__.displayName) {
      title = sessionData.state.__session_metadata__.displayName;
    }
    document.getElementById('activeAgentName').textContent = title;
    document.getElementById('activeAgentSub').textContent = `SESSION: ${activeAgent}`;

    // Render historical events
    if (sessionData.events && sessionData.events.length > 0) {
      const logEl = document.getElementById('chatLog');
      logEl.innerHTML = ''; // clear welcome message

      sessionData.events.forEach(ev => {
        if (ev.author === 'user' && ev.content && ev.content.parts) {
          ev.content.parts.forEach(part => {
            if (part.text) appendMessage('user', part.text);
          });
        } else if (ev.author === activeAgent && ev.content && ev.content.parts) {
          ev.content.parts.forEach(part => {
            if (part.text) appendMessage('agent', part.text);
          });
        } else if (ev.output && ev.nodeInfo) {
          // Render completed tool executions
          appendToolTrace(ev.nodeInfo.nodeName, ev.output);
        }
      });
    }

    // Update inspector contents
    updateStateInspector(sessionData.state);
    await loadArtifacts();
    await loadTelemetry();
  } catch (err) {
    showToast(`Failed to load session details: ${err.message}`, 'error');
  }
}

// Delete session
async function deleteSession(event, sessionId) {
  event.stopPropagation();
  if (!confirm(`Are you sure you want to delete session "${sessionId}"?`)) return;

  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${sessionId}`, {
      method: 'DELETE'
    });
    if (!res.ok) throw new Error('Failed to delete session');

    showToast('Session deleted', 'success');

    if (activeSessionId === sessionId) {
      activeSessionId = null;
      document.getElementById('sessionPill').textContent = 'no session';
      document.getElementById('sessionPill').className = 'pill';
      clearChatLog();
      clearInspector();
    }

    await loadSessions();
  } catch (err) {
    showToast(`Delete failed: ${err.message}`, 'error');
  }
}

// Create a new session
async function startNewSession() {
  if (!activeAgent) return;

  // Generate a random session ID
  const sessionId = 'session-' + Math.random().toString(36).substring(2, 10);
  
  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${sessionId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: {}, events: [] })
    });

    if (!res.ok) throw new Error('Failed to create session');
    const sessionData = await res.json(); // Session object

    activeSessionId = sessionData.id;
    await loadSessions();
    await selectSession(activeSessionId);
  } catch (err) {
    showToast(`Session Error: ${err.message}`, 'error');
  }
}

// Keydown handler
function handleInputKey(event) {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault();
    sendMessage();
  }
}

// Send user message with SSE Streaming
async function sendMessage() {
  const input = document.getElementById('messageInput');
  const text = input.value.trim();
  if (!text || !activeAgent || !activeSessionId) return;

  input.value = '';
  appendMessage('user', text);

  const indicator = document.getElementById('typingIndicator');
  indicator.classList.add('active');

  const payload = {
    appName: activeAgent,
    userId: 'user',
    sessionId: activeSessionId,
    newMessage: {
      role: 'user',
      parts: [{ text: text }]
    }
  };

  const isFirstMessage = document.querySelectorAll('.message-row.user').length === 1;
  if (isFirstMessage) {
    payload.stateDelta = {
      "__session_metadata__": {
        "displayName": text
      }
    };
  }

  try {
    const res = await fetch(`${API_BASE}/run_sse`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to start message execution');
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let agentMessageBubble = null;

    indicator.classList.remove('active');

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');

      for (let i = 0; i < lines.length - 1; i++) {
        const line = lines[i].trim();
        if (line.startsWith('data: ')) {
          const jsonStr = line.substring(6).trim();
          if (jsonStr) {
            try {
              const event = JSON.parse(jsonStr);

              // 1. Text Accumulation / Streaming
              if (event.author === activeAgent && event.content && event.content.parts) {
                event.content.parts.forEach(part => {
                  if (part.text) {
                    if (!agentMessageBubble) {
                      agentMessageBubble = appendMessagePlaceholder('agent');
                    }
                    updateMessageBubble(agentMessageBubble, part.text);
                  }
                });
              }

              // 2. Render completed tool executions inline
              if (event.output && event.nodeInfo) {
                appendToolTrace(event.nodeInfo.nodeName, event.output);
              }
            } catch (err) {
              console.error('SSE JSON error:', err, jsonStr);
            }
          }
        }
      }
      buffer = lines[lines.length - 1];
    }

    // Refresh state, artifacts, and telemetry
    await fetchSessionState();
    await loadArtifacts();
    await loadTelemetry();
    await loadSessions();
  } catch (err) {
    indicator.classList.remove('active');
    showToast(err.message, 'error');
    appendMessage('agent', `[Error: ${err.message}]`);
  }
}

// Fetch session state from DB
async function fetchSessionState() {
  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${activeSessionId}`);
    if (res.ok) {
      const sessionData = await res.json();
      updateStateInspector(sessionData.state);
    }
  } catch (err) {
    console.error('Failed to fetch session state:', err);
  }
}

// Helper: render state in sidebar
function updateStateInspector(state) {
  const inspector = document.getElementById('stateInspector');
  if (!state || Object.keys(state).length === 0) {
    inspector.innerHTML = '<span class="empty-state">No state variables loaded</span>';
    return;
  }
  inspector.textContent = JSON.stringify(state, null, 2);
}

// ---------- Artifacts Tab Logic ----------

async function loadArtifacts() {
  if (!activeAgent || !activeSessionId) return;
  const listEl = document.getElementById('artifactList');
  listEl.innerHTML = '<li style="padding: 10px; font-size: 11px; color: var(--text-faint);">Loading artifacts...</li>';

  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${activeSessionId}/artifacts`);
    if (!res.ok) throw new Error('Failed to load artifacts list');
    const filenames = await res.json(); // string array

    listEl.innerHTML = '';
    if (filenames.length === 0) {
      listEl.innerHTML = '<li style="padding: 10px; font-size: 11px; color: var(--text-faint); font-style: italic;">No artifacts generated in this session</li>';
      return;
    }

    filenames.forEach(name => {
      const li = document.createElement('li');
      li.className = 'artifact-row';
      li.onclick = () => viewArtifact(name);
      li.innerHTML = `📄 <span style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${escapeHtml(name)}</span>`;
      listEl.appendChild(li);
    });
  } catch (err) {
    listEl.innerHTML = `<li style="padding: 10px; font-size: 11px; color: var(--danger);">${err.message}</li>`;
  }
}

async function viewArtifact(artifactName) {
  // Highlight active row
  document.querySelectorAll('.artifact-row').forEach(row => {
    const text = row.querySelector('span').textContent;
    row.className = `artifact-row ${text === artifactName ? 'active' : ''}`;
  });

  const titleEl = document.getElementById('artifactViewerTitle');
  const codeEl = document.getElementById('artifactViewerCode');
  titleEl.textContent = 'Loading...';
  codeEl.textContent = '';

  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/user/sessions/${activeSessionId}/artifacts/${artifactName}`);
    if (!res.ok) throw new Error('Failed to load artifact content');
    const part = await res.json(); // genai.Part object (containing text representation)

    titleEl.textContent = artifactName;
    if (part && part.text) {
      codeEl.textContent = part.text;
    } else {
      codeEl.textContent = '[Binary data or empty content]';
    }
  } catch (err) {
    titleEl.textContent = 'Error';
    codeEl.textContent = `Failed to preview artifact: ${err.message}`;
  }
}

// ---------- Telemetry Tab Logic ----------

async function loadTelemetry() {
  if (!activeAgent || !activeSessionId) return;
  const listEl = document.getElementById('telemetryTimeline');
  listEl.innerHTML = '<span class="empty-state">Loading telemetry traces...</span>';

  try {
    const res = await fetch(`${API_BASE}/debug/trace/session/${activeSessionId}`);
    if (!res.ok) throw new Error('Failed to load telemetry spans');
    const spans = await res.json(); // Array of DebugSpan objects

    listEl.innerHTML = '';
    if (spans.length === 0) {
      listEl.innerHTML = '<span class="empty-state">No telemetry data captured</span>';
      return;
    }

    // Sort by StartTime ascending
    spans.sort((a, b) => new Date(a.start_time) - new Date(b.start_time));

    spans.forEach(span => {
      const card = document.createElement('div');
      card.className = 'trace-card';

      const start = new Date(span.start_time);
      const end = new Date(span.end_time);
      const latencyMs = end - start;

      let detailsHTML = '';
      if (span.name === 'generate_content') {
        const modelName = span.attributes['gen_ai.response.model'] || span.attributes['gen_ai.request.model'] || 'Gemini';
        const promptTokens = span.attributes['gen_ai.usage.prompt_tokens'] || 0;
        const completionTokens = span.attributes['gen_ai.usage.completion_tokens'] || 0;

        detailsHTML = `
          <div class="trace-details">
            <span>Model: <strong>${modelName}</strong></span>
            <span>Input Tokens: <strong>${promptTokens}</strong></span>
            <span>Output Tokens: <strong>${completionTokens}</strong></span>
          </div>
        `;
      } else if (span.name === 'execute_tool') {
        const toolName = span.attributes['gen_ai.tool.name'] || 'Custom Tool';
        detailsHTML = `
          <div class="trace-details">
            <span>Tool: <strong>${toolName}</strong></span>
          </div>
        `;
      }

      let logsHTML = '';
      if (span.logs && span.logs.length > 0) {
        logsHTML = `
          <div class="trace-logs">
            ${span.logs.map(log => `[${new Date(log.timestamp).toLocaleTimeString()}] ${escapeHtml(log.body)}`).join('\n')}
          </div>
        `;
      }

      card.innerHTML = `
        <div class="trace-card-header">
          <span class="trace-name">${escapeHtml(span.name)}</span>
          <span class="trace-latency">${latencyMs}ms</span>
        </div>
        ${detailsHTML}
        ${logsHTML}
      `;
      listEl.appendChild(card);
    });

  } catch (err) {
    listEl.innerHTML = `<span class="empty-state" style="color: var(--danger);">Telemetry error: ${err.message}</span>`;
  }
}

// ---------- UI layout / formatting helpers ----------

function toggleInspector() {
  const drawer = document.getElementById('inspectorDrawer');
  drawer.classList.toggle('active');
}

function switchTab(event, tabName) {
  // Toggle tab buttons
  document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.classList.remove('active');
  });
  event.target.classList.add('active');

  // Toggle tab content panels
  document.querySelectorAll('.tab-content').forEach(panel => {
    panel.classList.remove('active');
  });
  document.getElementById(`tab-${tabName}`).classList.add('active');
  activeTab = tabName;
}

function clearChatLog() {
  const log = document.getElementById('chatLog');
  log.innerHTML = `
    <div class="welcome-message">
      <h2>Welcome to Botson Custom Chat</h2>
      <p>Choose an agent from the dropdown on the left to start a dynamic chat thread. Any tool executions or background agent reasoning will render inline in real-time!</p>
    </div>
  `;
}

function clearInspector() {
  document.getElementById('stateInspector').innerHTML = '<span class="empty-state">No state keys loaded</span>';
  document.getElementById('artifactList').innerHTML = '<li style="padding: 10px; font-size: 11px; color: var(--text-faint); font-style: italic;">No artifacts loaded</li>';
  document.getElementById('artifactViewerTitle').textContent = 'No artifact loaded';
  document.getElementById('artifactViewerCode').innerHTML = '<span class="empty-state">Select an artifact to preview its content</span>';
  document.getElementById('telemetryTimeline').innerHTML = '<span class="empty-state">No telemetry data captured</span>';
}

// Helper: append placeholder for streaming response
function appendMessagePlaceholder(sender) {
  const log = document.getElementById('chatLog');
  const row = document.createElement('div');
  row.className = `message-row ${sender}`;
  
  const bubble = document.createElement('div');
  bubble.className = 'message-bubble';
  
  row.appendChild(bubble);
  log.appendChild(row);
  log.scrollTop = log.scrollHeight;
  return bubble;
}

// Helper: update contents of a message bubble
function updateMessageBubble(bubble, text) {
  bubble.innerHTML = '';
  const paragraphs = text.split('\n\n');
  paragraphs.forEach(p => {
    const pg = document.createElement('p');
    pg.textContent = p;
    bubble.appendChild(pg);
  });
  const log = document.getElementById('chatLog');
  log.scrollTop = log.scrollHeight;
}

// Helper: append chat bubbles
function appendMessage(sender, text) {
  const bubble = appendMessagePlaceholder(sender);
  updateMessageBubble(bubble, text);
}

// Helper: append tool logs
function appendToolTrace(toolName, output) {
  const log = document.getElementById('chatLog');
  const row = document.createElement('div');
  row.className = 'message-row system';

  const card = document.createElement('div');
  card.className = 'tool-trace-card';

  let outputStr = '';
  if (typeof output === 'string') {
    outputStr = output;
  } else {
    outputStr = JSON.stringify(output, null, 2);
  }

  card.innerHTML = `
    <div class="tool-header">
      <span class="tool-name-pill">⚙ ${escapeHtml(toolName)}</span>
      <span class="tool-status">execution completed</span>
    </div>
    <div class="tool-body">${escapeHtml(outputStr)}</div>
  `;

  row.appendChild(card);
  log.appendChild(row);
  log.scrollTop = log.scrollHeight;
}

// Toast alerts
function showToast(message, type = 'success') {
  const container = document.getElementById('toastContainer');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.innerText = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.remove();
  }, 4000);
}

// Escapes
function escapeHtml(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}
