const API_BASE = 'http://localhost:8080/api';
let activeAgent = null;
let activeSessionId = null;

// Initial bootstrap
window.addEventListener('DOMContentLoaded', async () => {
  await loadAgents();
});

// Load registered agents
async function loadAgents() {
  const selector = document.getElementById('agentSelector');
  selector.innerHTML = '<option value="">Loading agents...</option>';

  try {
    const res = await fetch(`${API_BASE}/apps`);
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
    return;
  }

  document.getElementById('activeAgentName').textContent = activeAgent;
  document.getElementById('activeAgentSub').textContent = 'ACTIVE AGENT';
  
  // Clear chat log and state inspector
  const log = document.getElementById('chatLog');
  log.innerHTML = `
    <div class="welcome-message">
      <h2>Chat started with ${activeAgent}</h2>
      <p>Send a message below to start your conversation. Dynamic session state and active tool traces will update in real-time.</p>
    </div>
  `;

  document.getElementById('stateInspector').innerHTML = '<span class="empty-state">Initializing session...</span>';

  await startNewSession();
}

// Create a new session
async function startNewSession() {
  if (!activeAgent) return;

  // Generate a random session ID
  const sessionId = 'session-' + Math.random().toString(36).substring(2, 10);
  
  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/default/sessions/${sessionId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: {}, events: [] })
    });

    if (!res.ok) throw new Error('Failed to create session');
    const sessionData = await res.json(); // Session object

    activeSessionId = sessionData.id;
    document.getElementById('sessionPill').textContent = activeSessionId;
    document.getElementById('sessionPill').className = 'pill pill-accent';

    updateStateInspector(sessionData.state);
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

// Send user message
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
    userId: 'default',
    sessionId: activeSessionId,
    newMessage: {
      role: 'user',
      parts: [{ text: text }]
    }
  };

  try {
    const res = await fetch(`${API_BASE}/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to get agent response');
    }

    const events = await res.json(); // Array of Event objects
    indicator.classList.remove('active');

    // Parse returned events
    events.forEach(ev => {
      // 1. Render Model/Agent text responses
      if (ev.author === activeAgent && ev.content && ev.content.parts) {
        ev.content.parts.forEach(part => {
          if (part.text) {
            appendMessage('agent', part.text);
          }
        });
      }
      
      // 2. Render Tool Execution Cards
      if (ev.output && ev.nodeInfo) {
        appendToolTrace(ev.nodeInfo.nodeName, ev.output);
      }
    });

    // Fetch updated session state
    await fetchSessionState();
  } catch (err) {
    indicator.classList.remove('active');
    showToast(err.message, 'error');
    appendMessage('agent', `[Error: ${err.message}]`);
  }
}

// Fetch session state from DB
async function fetchSessionState() {
  try {
    const res = await fetch(`${API_BASE}/apps/${activeAgent}/users/default/sessions/${activeSessionId}`);
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

// Helper: append chat bubbles
function appendMessage(sender, text) {
  const log = document.getElementById('chatLog');
  const row = document.createElement('div');
  row.className = `message-row ${sender}`;
  
  const bubble = document.createElement('div');
  bubble.className = 'message-bubble';
  
  // Format simple markdown-like newlines
  const paragraphs = text.split('\n\n');
  paragraphs.forEach(p => {
    const pg = document.createElement('p');
    pg.textContent = p;
    bubble.appendChild(pg);
  });

  row.appendChild(bubble);
  log.appendChild(row);
  log.scrollTop = log.scrollHeight;
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
