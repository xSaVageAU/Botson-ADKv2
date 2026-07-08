// Chat Console View Controller

window.loadAgentsForChat = async function() {
  const selector = document.getElementById('agentSelector');
  if (!selector) return;
  selector.innerHTML = '<option value="">Loading agents...</option>';

  try {
    const res = await fetch('/api/list-apps');
    if (!res.ok) throw new Error('Failed to load apps');
    const apps = await res.json();

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

    if (window.activeAgent && apps.includes(window.activeAgent)) {
      selector.value = window.activeAgent;
    } else {
      window.activeAgent = selector.value;
    }

    await window.switchAgent();
  } catch (err) {
    window.showToast(`API Connection Error: ${err.message}`, 'error');
    selector.innerHTML = '<option value="">Connection error</option>';
  }
};

window.switchAgent = async function() {
  const selector = document.getElementById('agentSelector');
  if (!selector) return;
  window.activeAgent = selector.value;

  if (!window.activeAgent) {
    document.getElementById('activeAgentName').textContent = 'No Agent Selected';
    document.getElementById('activeAgentSub').textContent = 'AGENT';
    document.getElementById('sessionPill').textContent = 'no session';
    document.getElementById('sessionPill').className = 'pill';
    document.getElementById('sessionList').innerHTML = '';
    document.getElementById('sessionCount').textContent = '0';
    return;
  }

  document.getElementById('activeAgentName').textContent = window.activeAgent;
  document.getElementById('activeAgentSub').textContent = 'ACTIVE AGENT';
  
  window.clearChatLog();
  window.clearInspector();

  await window.loadSessions();
};

window.loadSessions = async function() {
  if (!window.activeAgent) return;
  const listEl = document.getElementById('sessionList');
  if (!listEl) return;
  listEl.innerHTML = '<li style="padding: 10px; font-size: 12px; color: var(--text-faint);">Loading sessions...</li>';

  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions`);
    if (!res.ok) throw new Error('Failed to load sessions');
    let sessions = await res.json();
    if (!sessions) sessions = [];

    listEl.innerHTML = '';
    document.getElementById('sessionCount').textContent = sessions.length;

    if (sessions.length === 0) {
      listEl.innerHTML = '<li style="padding: 10px; font-size: 12px; color: var(--text-faint); font-style: italic;">No active sessions found</li>';
      if (!window.activeSessionId) {
        await window.startNewSession();
      }
      return;
    }

    sessions.sort((a, b) => b.lastUpdateTime - a.lastUpdateTime);

    sessions.forEach(s => {
      const li = document.createElement('li');
      li.className = `session-row ${window.activeSessionId === s.id ? 'active' : ''}`;
      li.dataset.id = s.id;
      li.onclick = () => window.selectSession(s.id);

      const timeStr = new Date(s.lastUpdateTime * 1000).toLocaleString();
      let title = s.id;
      if (s.state && s.state.__session_metadata__ && s.state.__session_metadata__.displayName) {
        title = s.state.__session_metadata__.displayName;
      }

      li.innerHTML = `
        <div class="session-info">
          <span class="session-id-text" title="${window.escapeHtml(s.id)}">${window.escapeHtml(title)}</span>
          <span class="session-time-text">${timeStr}</span>
        </div>
        <button class="btn-delete-session" onclick="window.deleteSession(event, '${s.id}')" title="Delete session">🗑</button>
      `;
      listEl.appendChild(li);
    });

    if (window.activeSessionId) {
      const activeRow = listEl.querySelector(`.session-row[data-id="${window.activeSessionId}"]`);
      if (activeRow) {
        activeRow.classList.add('active');
      } else {
        window.selectSession(sessions[0].id);
      }
    } else if (sessions.length > 0) {
      window.selectSession(sessions[0].id);
    }
  } catch (err) {
    window.showToast(`Failed to load sessions: ${err.message}`, 'error');
  }
};

window.selectSession = async function(sessionId) {
  window.activeSessionId = sessionId;
  window.isNewSession = false;
  
  document.querySelectorAll('.session-row').forEach(row => {
    row.classList.toggle('active', row.dataset.id === sessionId);
  });

  document.getElementById('sessionPill').textContent = sessionId;
  document.getElementById('sessionPill').className = 'pill pill-accent';

  window.clearChatLog();
  window.clearInspector();

  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${sessionId}`);
    if (!res.ok) throw new Error('Failed to load session details');
    const sessionData = await res.json();

    let title = sessionData.id;
    if (sessionData.state && sessionData.state.__session_metadata__ && sessionData.state.__session_metadata__.displayName) {
      title = sessionData.state.__session_metadata__.displayName;
    }
    document.getElementById('activeAgentName').textContent = title;
    document.getElementById('activeAgentSub').textContent = `SESSION: ${window.activeAgent}`;

    // Enable chat input initially
    const chatInput = document.getElementById('chatInput');
    if (chatInput) {
      chatInput.disabled = false;
      chatInput.placeholder = "Type a message... (Ctrl+Enter to send)";
    }

    if (sessionData.events && sessionData.events.length > 0) {
      const logEl = document.getElementById('chatLog');
      logEl.innerHTML = ''; // clear welcome

      // 1. Scan chronologically to build lookup tables keyed by function
      // call id: which adk_request_confirmation calls have been answered,
      // and what every regular tool call's result actually was. Without
      // the second table, a saved (i.e. already-finished) tool call has
      // no way to be told apart from one still in flight -- which is why
      // this used to always render the "running" indicator forever, even
      // for a call that completed the moment it happened.
      const answeredConfirmations = {};
      const functionResults = {};
      sessionData.events.forEach(ev => {
        if (ev.content && ev.content.parts) {
          ev.content.parts.forEach(part => {
            if (!part.functionResponse) return;
            if (part.functionResponse.name === 'adk_request_confirmation') {
              const resp = part.functionResponse.response || {};
              answeredConfirmations[part.functionResponse.id] = {
                confirmed: resp.confirmed === true || resp.confirmed === 'true',
                responder: ev.author
              };
            } else {
              functionResults[part.functionResponse.id] = part.functionResponse.response;
            }
          });
        }
      });

      // 2. Render all events
      sessionData.events.forEach(ev => {
        if (ev.author === 'user' && ev.content && ev.content.parts) {
          // Only render standard text messages from user, skipping function responses
          let textParts = ev.content.parts.filter(part => part.text);
          if (textParts.length > 0) {
            textParts.forEach(part => {
              window.appendMessage('user', part.text);
            });
          }
        } else if (ev.author === window.activeAgent && ev.content && ev.content.parts) {
          ev.content.parts.forEach(part => {
            if (part.text) {
              window.appendMessage('agent', part.text);
            }
            if (part.functionCall) {
              if (part.functionCall.name === 'adk_request_confirmation') {
                const callId = part.functionCall.id;
                if (answeredConfirmations[callId]) {
                  window.appendHitlResolved(part.functionCall, answeredConfirmations[callId].confirmed);
                } else {
                  window.appendHitlPending(part.functionCall);
                }
              } else if (Object.prototype.hasOwnProperty.call(functionResults, part.functionCall.id)) {
                // A saved conversation only ever contains finished calls --
                // render the actual result, collapsed by default, same as
                // a live trace.
                window.appendToolTrace(part.functionCall.name, JSON.stringify(functionResults[part.functionCall.id]));
              } else {
                // No matching response was found (e.g. the process was
                // interrupted mid-call) -- say so rather than showing a
                // spinner that will never resolve.
                window.appendToolTrace(part.functionCall.name, '(no result recorded)');
              }
            }
          });
        } else if (ev.output && ev.nodeInfo) {
          window.appendToolTrace(ev.nodeInfo.nodeName, ev.output);
        }
      });
    }

    window.updateStateInspector(sessionData.state);
    await window.loadArtifacts();
    await window.loadTelemetry();
  } catch (err) {
    window.showToast(`Failed to load session details: ${err.message}`, 'error');
  }
};

window.startNewSession = async function() {
  if (!window.activeAgent) return;

  window.activeSessionId = window.generateUUID();
  window.isNewSession = true;

  document.getElementById('sessionPill').textContent = window.activeSessionId;
  document.getElementById('sessionPill').className = 'pill pill-accent';
  document.getElementById('activeAgentName').textContent = 'New Chat';
  document.getElementById('activeAgentSub').textContent = `SESSION: ${window.activeAgent}`;

  window.clearChatLog();
  window.clearInspector();

  const listEl = document.getElementById('sessionList');
  if (listEl) {
    const emptyItem = listEl.querySelector('.empty-state') || listEl.querySelector('li[style*="italic"]');
    if (emptyItem) emptyItem.remove();

    document.querySelectorAll('.session-row').forEach(row => row.classList.remove('active'));

    const li = document.createElement('li');
    li.className = 'session-row active';
    li.dataset.id = window.activeSessionId;
    li.onclick = () => window.selectSession(window.activeSessionId);
    li.innerHTML = `
      <div class="session-info">
        <span class="session-id-text" title="${window.escapeHtml(window.activeSessionId)}">New Chat</span>
        <span class="session-time-text">just now</span>
      </div>
      <button class="btn-delete-session" onclick="window.deleteSession(event, '${window.activeSessionId}')" title="Delete session">🗑</button>
    `;
    listEl.insertBefore(li, listEl.firstChild);
  }
};

window.deleteSession = async function(event, sessionId) {
  event.stopPropagation();
  if (!confirm(`Are you sure you want to delete session "${sessionId}"?`)) return;

  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${sessionId}`, {
      method: 'DELETE'
    });
    if (!res.ok) throw new Error('Failed to delete session');

    window.showToast('Session deleted', 'success');

    if (window.activeSessionId === sessionId) {
      window.activeSessionId = null;
      document.getElementById('sessionPill').textContent = 'no session';
      document.getElementById('sessionPill').className = 'pill';
      window.clearChatLog();
      window.clearInspector();
    }

    await window.loadSessions();
  } catch (err) {
    window.showToast(`Delete failed: ${err.message}`, 'error');
  }
};

window.handleInputKey = function(event) {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault();
    window.sendMessage();
  }
};

window.sendMessage = async function() {
  const input = document.getElementById('messageInput');
  if (!input) return;
  const text = input.value.trim();
  if (!text || !window.activeAgent || !window.activeSessionId) return;

  input.value = '';
  window.appendMessage('user', text);

  const indicator = document.getElementById('typingIndicator');
  if (indicator) indicator.classList.add('active');

  const payload = {
    appName: window.activeAgent,
    userId: window.currentUser,
    sessionId: window.activeSessionId,
    newMessage: {
      role: 'user',
      parts: [{ text: text }]
    }
  };

  if (window.isNewSession) {
    try {
      const createRes = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${window.activeSessionId}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          state: {
            "__session_metadata__": {
              "displayName": text
            }
          },
          events: []
        })
      });
      if (!createRes.ok) throw new Error('Failed to initialize session');
      window.isNewSession = false;
    } catch (err) {
      if (indicator) indicator.classList.remove('active');
      window.showToast(`Failed to initialize session: ${err.message}`, 'error');
      return;
    }
  }

  try {
    const res = await fetch('/api/run_sse', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed execution');
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let agentMessageBubble = null;

    if (indicator) indicator.classList.remove('active');

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

              if (event.author === window.activeAgent && event.content && event.content.parts) {
                event.content.parts.forEach(part => {
                  if (part.text) {
                    if (!agentMessageBubble) {
                      agentMessageBubble = window.appendMessagePlaceholder('agent');
                    }
                    window.updateMessageBubble(agentMessageBubble, part.text);
                  }
                  // Live "running" feedback for a plain tool call (not the
                  // HITL confirmation tool, which has its own pending-card
                  // flow). It's replaced by the real completed trace when
                  // selectSession() re-renders the whole log from the
                  // persisted session further down -- nothing needs to
                  // remove it here.
                  if (part.functionCall && part.functionCall.name !== 'adk_request_confirmation') {
                    window.appendToolCallIndication(part.functionCall.name);
                  }
                });
              }

              if (event.output && event.nodeInfo) {
                window.appendToolTrace(event.nodeInfo.nodeName, event.output);
              }
            } catch (err) {
              console.error('SSE parsing error:', err, jsonStr);
            }
          }
        }
      }
      buffer = lines[lines.length - 1];
    }

    await window.fetchSessionState();
    await window.loadArtifacts();
    await window.loadTelemetry();
    await window.loadSessions();
    await window.selectSession(window.activeSessionId);
  } catch (err) {
    if (indicator) indicator.classList.remove('active');
    window.showToast(err.message, 'error');
    window.appendMessage('agent', `[Error: ${err.message}]`);
  }
};

window.fetchSessionState = async function() {
  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${window.activeSessionId}`);
    if (res.ok) {
      const sessionData = await res.json();
      window.updateStateInspector(sessionData.state);
    }
  } catch (err) {
    console.error('State inspect error:', err);
  }
};

window.updateStateInspector = function(state) {
  const inspector = document.getElementById('stateInspector');
  if (!inspector) return;
  if (!state || Object.keys(state).length === 0) {
    inspector.innerHTML = '<span class="empty-state">No state keys loaded</span>';
    return;
  }
  inspector.textContent = JSON.stringify(state, null, 2);
};

// Artifacts tab details
window.loadArtifacts = async function() {
  if (!window.activeAgent || !window.activeSessionId) return;
  const listEl = document.getElementById('artifactList');
  if (!listEl) return;
  listEl.innerHTML = '<li style="padding: 10px; font-size: 11px; color: var(--text-faint);">Loading artifacts...</li>';

  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${window.activeSessionId}/artifacts`);
    if (!res.ok) throw new Error('Failed to load artifacts');
    const filenames = await res.json();

    listEl.innerHTML = '';
    if (filenames.length === 0) {
      listEl.innerHTML = '<li style="padding: 10px; font-size: 11px; color: var(--text-faint); font-style: italic;">No artifacts generated</li>';
      return;
    }

    filenames.forEach(name => {
      const li = document.createElement('li');
      li.className = 'artifact-row';
      li.onclick = () => window.viewArtifact(name);
      li.innerHTML = `📄 <span style="flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${window.escapeHtml(name)}</span>`;
      listEl.appendChild(li);
    });
  } catch (err) {
    listEl.innerHTML = `<li style="padding: 10px; font-size: 11px; color: var(--danger);">${err.message}</li>`;
  }
};

window.viewArtifact = async function(artifactName) {
  document.querySelectorAll('.artifact-row').forEach(row => {
    const text = row.querySelector('span').textContent;
    row.className = `artifact-row ${text === artifactName ? 'active' : ''}`;
  });

  const titleEl = document.getElementById('artifactViewerTitle');
  const codeEl = document.getElementById('artifactViewerCode');
  titleEl.textContent = 'Loading...';
  codeEl.textContent = '';

  try {
    const res = await fetch(`/api/apps/${window.activeAgent}/users/${window.currentUser}/sessions/${window.activeSessionId}/artifacts/${artifactName}`);
    if (!res.ok) throw new Error('Failed to load content');
    const part = await res.json();

    titleEl.textContent = artifactName;
    if (part && part.text) {
      codeEl.textContent = part.text;
    } else {
      codeEl.textContent = '[Binary data or empty content]';
    }
  } catch (err) {
    titleEl.textContent = 'Error';
    codeEl.textContent = err.message;
  }
};

// Telemetry Timeline
window.loadTelemetry = async function() {
  if (!window.activeAgent || !window.activeSessionId) return;
  const listEl = document.getElementById('telemetryTimeline');
  if (!listEl) return;
  listEl.innerHTML = '<span class="empty-state">Loading traces...</span>';

  try {
    const res = await fetch(`/api/debug/trace/session/${window.activeSessionId}`);
    if (!res.ok) throw new Error('Failed to load spans');
    const spans = await res.json();

    listEl.innerHTML = '';
    if (spans.length === 0) {
      listEl.innerHTML = '<span class="empty-state">No telemetry traces</span>';
      return;
    }

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
      if (span.events && span.events.length > 0) {
        logsHTML = `<pre class="trace-logs">${span.events.map(ev => `[${ev.time.substring(11, 19)}] ${ev.name}: ${JSON.stringify(ev.attributes)}`).join('\n')}</pre>`;
      }

      card.innerHTML = `
        <div class="trace-card-header">
          <span class="trace-name">${window.escapeHtml(span.name)}</span>
          <span class="trace-latency">${latencyMs}ms</span>
        </div>
        ${detailsHTML}
        ${logsHTML}
      `;
      listEl.appendChild(card);
    });
  } catch (err) {
    listEl.innerHTML = `<span class="empty-state" style="color: var(--danger);">${err.message}</span>`;
  }
};

window.toggleInspector = function() {
  const drawer = document.getElementById('inspectorDrawer');
  if (drawer) drawer.classList.toggle('active');
};

window.switchInspectorTab = function(tabId) {
  window.activeInspectorTab = tabId;
  document.querySelectorAll('#inspectorDrawer .tab-btn').forEach(btn => {
    btn.classList.toggle('active', btn.id === `btn-tab-${tabId}`);
  });
  document.querySelectorAll('#inspectorDrawer .tab-content').forEach(content => {
    content.classList.toggle('active', content.id === `tab-${tabId}`);
  });
};

window.clearChatLog = function() {
  const chatLog = document.getElementById('chatLog');
  if (!chatLog) return;
  chatLog.innerHTML = `
    <div class="welcome-message">
      <h2>Welcome to Botson Custom Chat</h2>
      <p>Choose an agent from the dropdown on the left to start a dynamic chat thread. Any tool executions or background agent reasoning will render inline in real-time!</p>
    </div>
  `;
};

window.clearInspector = function() {
  const stateInspector = document.getElementById('stateInspector');
  if (stateInspector) stateInspector.innerHTML = '<span class="empty-state">No state keys loaded</span>';
  
  const artifactList = document.getElementById('artifactList');
  if (artifactList) artifactList.innerHTML = '';
  
  const titleEl = document.getElementById('artifactViewerTitle');
  if (titleEl) titleEl.textContent = 'No artifact loaded';
  
  const versionEl = document.getElementById('artifactViewerVersion');
  if (versionEl) versionEl.textContent = '';
  
  const codeEl = document.getElementById('artifactViewerCode');
  if (codeEl) codeEl.innerHTML = '<span class="empty-state">Select an artifact to preview its content</span>';
  
  const timeline = document.getElementById('telemetryTimeline');
  if (timeline) timeline.innerHTML = '<span class="empty-state">No telemetry data captured</span>';
};

window.appendMessage = function(role, text) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return;
  
  const welcome = logEl.querySelector('.welcome-message');
  if (welcome) welcome.remove();

  const row = document.createElement('div');
  row.className = `message-row ${role === 'user' ? 'user' : 'agent'}`;

  const bubble = document.createElement('div');
  bubble.className = 'message-bubble';
  bubble.textContent = text;

  row.appendChild(bubble);
  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;
};

window.appendMessagePlaceholder = function(role) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return null;
  const welcome = logEl.querySelector('.welcome-message');
  if (welcome) welcome.remove();

  const row = document.createElement('div');
  row.className = `message-row ${role === 'user' ? 'user' : 'agent'}`;

  const bubble = document.createElement('div');
  bubble.className = 'message-bubble';
  
  row.appendChild(bubble);
  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;
  return bubble;
};

window.updateMessageBubble = function(bubble, newText) {
  bubble.textContent = (bubble.textContent || '') + newText;
  const logEl = document.getElementById('chatLog');
  if (logEl) logEl.scrollTop = logEl.scrollHeight;
};

window.appendToolTrace = function(name, output) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return;
  const welcome = logEl.querySelector('.welcome-message');
  if (welcome) welcome.remove();

  const row = document.createElement('div');
  row.className = 'message-row system';

  const card = document.createElement('div');
  card.className = 'tool-trace-card';

  let formattedOutput = '';
  try {
    const parsed = JSON.parse(output);
    formattedOutput = JSON.stringify(parsed, null, 2);
  } catch (err) {
    formattedOutput = output;
  }

  card.innerHTML = `
    <button class="tool-trace-toggle" type="button">
      <span class="tool-trace-icon">⚙</span>
      <span class="tool-trace-name">${window.escapeHtml(name)}</span>
      <span class="tool-trace-status">Completed</span>
      <span class="tool-trace-chevron">▸</span>
    </button>
    <pre class="tool-body">${window.escapeHtml(formattedOutput)}</pre>
  `;
  card.querySelector('.tool-trace-toggle').onclick = () => card.classList.toggle('expanded');

  row.appendChild(card);
  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;
};

window.appendToolCallIndication = function(toolName) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return;
  const row = document.createElement('div');
  row.className = 'message-row system';
  row.innerHTML = `
    <div class="tool-pill">
      <span class="tool-pill-icon">⚙</span>
      <span class="tool-pill-name">${window.escapeHtml(toolName)}</span>
      <span class="tool-pill-spinner"></span>
    </div>
  `;
  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;
};

window.appendHitlResolved = function(fc, confirmed) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return;

  const row = document.createElement('div');
  row.className = 'message-row system';

  const toolName = fc.args.originalFunctionCall?.name || 'tool';
  const pill = document.createElement('div');
  pill.className = `hitl-resolved-pill ${confirmed ? 'hitl-resolved-approved' : 'hitl-resolved-denied'}`;
  pill.innerHTML = `
    <span class="hitl-resolved-icon">${confirmed ? '✓' : '✗'}</span>
    <span class="hitl-resolved-name">${window.escapeHtml(toolName)}</span>
    <span>${confirmed ? 'approved' : 'denied'}</span>
  `;

  row.appendChild(pill);
  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;
};

window.appendHitlPending = function(fc) {
  const logEl = document.getElementById('chatLog');
  if (!logEl) return;

  const row = document.createElement('div');
  row.className = 'message-row system';

  const card = document.createElement('div');
  card.className = 'hitl-card';
  card.id = `hitl-${fc.id}`;

  const toolName = fc.args.originalFunctionCall?.name || 'tool';

  const header = document.createElement('div');
  header.className = 'hitl-header';
  header.innerHTML = `
    <span class="hitl-icon">⚠</span>
    <div class="hitl-heading">
      <span class="hitl-title">Permission requested</span>
      <span class="hitl-subtitle">${window.escapeHtml(toolName)}</span>
    </div>
  `;

  const body = document.createElement('div');
  body.className = 'hitl-body';
  body.textContent = fc.args.hint || 'The agent requires approval to execute this tool.';

  const detailsWrap = document.createElement('details');
  detailsWrap.className = 'hitl-details-wrap';
  const summary = document.createElement('summary');
  summary.textContent = 'View arguments';
  const details = document.createElement('pre');
  details.className = 'hitl-details';
  details.textContent = JSON.stringify(fc.args.originalFunctionCall || {}, null, 2);
  detailsWrap.appendChild(summary);
  detailsWrap.appendChild(details);

  const actions = document.createElement('div');
  actions.className = 'hitl-actions';

  const denyBtn = document.createElement('button');
  denyBtn.className = 'hitl-btn hitl-btn-deny';
  denyBtn.textContent = 'Deny';
  denyBtn.onclick = () => window.sendConfirmation(fc.id, false);

  const approveBtn = document.createElement('button');
  approveBtn.className = 'hitl-btn hitl-btn-approve';
  approveBtn.textContent = 'Approve';
  approveBtn.onclick = () => window.sendConfirmation(fc.id, true);

  actions.appendChild(denyBtn);
  actions.appendChild(approveBtn);

  card.appendChild(header);
  card.appendChild(body);
  card.appendChild(detailsWrap);
  card.appendChild(actions);
  row.appendChild(card);

  logEl.appendChild(row);
  logEl.scrollTop = logEl.scrollHeight;

  // Disable chat input
  const chatInput = document.getElementById('chatInput');
  if (chatInput) {
    chatInput.disabled = true;
    chatInput.placeholder = "Agent is waiting for approval...";
  }
};

window.sendConfirmation = async function(callId, confirmed) {
  const indicator = document.getElementById('typingIndicator');
  if (indicator) indicator.classList.add('active');

  // Disable verification buttons
  document.querySelectorAll('.hitl-btn').forEach(btn => btn.disabled = true);

  const payload = {
    appName: window.activeAgent,
    userId: window.currentUser,
    sessionId: window.activeSessionId,
    newMessage: {
      role: 'user',
      parts: [{
        functionResponse: {
          name: 'adk_request_confirmation',
          id: callId,
          response: {
            confirmed: confirmed
          }
        }
      }]
    }
  };

  try {
    const res = await fetch('/api/run_sse', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (!res.ok) {
      const errMsg = await res.text();
      throw new Error(errMsg || 'Failed to submit confirmation');
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let agentMessageBubble = null;

    if (indicator) indicator.classList.remove('active');

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

              if (event.author === window.activeAgent && event.content && event.content.parts) {
                event.content.parts.forEach(part => {
                  if (part.text) {
                    if (!agentMessageBubble) {
                      agentMessageBubble = window.appendMessagePlaceholder('agent');
                    }
                    window.updateMessageBubble(agentMessageBubble, part.text);
                  }
                  // Live "running" feedback for a plain tool call (not the
                  // HITL confirmation tool, which has its own pending-card
                  // flow). It's replaced by the real completed trace when
                  // selectSession() re-renders the whole log from the
                  // persisted session further down -- nothing needs to
                  // remove it here.
                  if (part.functionCall && part.functionCall.name !== 'adk_request_confirmation') {
                    window.appendToolCallIndication(part.functionCall.name);
                  }
                });
              }

              if (event.output && event.nodeInfo) {
                window.appendToolTrace(event.nodeInfo.nodeName, event.output);
              }
            } catch (err) {
              console.error('SSE parsing error:', err, jsonStr);
            }
          }
        }
      }
      buffer = lines[lines.length - 1];
    }
  } catch (err) {
    if (indicator) indicator.classList.remove('active');
    window.showToast(`Confirmation failed: ${err.message}`, 'error');
  } finally {
    // Reload history to update cards status
    await window.selectSession(window.activeSessionId);
    await window.loadSessions();
  }
};
