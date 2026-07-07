// visual workflow studio javascript controller

(function () {
  let workflows = [];
  let currentWorkflow = null;
  let selectedNodeId = null;

  // Canvas interaction variables
  let isDraggingCanvas = false;
  let canvasX = 0;
  let canvasY = 0;
  let zoomLevel = 1;

  // Node drag variables
  let dragNode = null;
  let dragOffsetX = 0;
  let dragOffsetY = 0;

  // Port link drag variables
  let activeLinkPort = null;
  let tempLinkLine = null;

  // Available agent names and tool names for dropdown selection
  let availableAgents = [];
  let availableTools = ["saveArtifact", "readFile", "listFiles"]; // default tools

  window.loadWorkflows = async function () {
    try {
      // 1. Fetch available agents to fill dropdowns
      const agentsRes = await fetch('/botson/api/agents');
      if (agentsRes.ok) {
        const agents = await agentsRes.json();
        availableAgents = agents.map(a => a.name);
      }

      // 2. Fetch workflows
      const res = await fetch('/botson/api/workflows');
      if (!res.ok) throw new Error('Failed to load workflows list');
      workflows = await res.json();
      renderWorkflowSidebar();
    } catch (err) {
      window.showToast(`Error: ${err.message}`, 'error');
    }
  };

  function renderWorkflowSidebar() {
    const listEl = document.getElementById('workflowList');
    const countEl = document.getElementById('workflowCount');
    if (!listEl) return;

    listEl.innerHTML = '';
    countEl.textContent = workflows.length;

    workflows.forEach(wf => {
      const li = document.createElement('li');
      li.className = 'node-row';
      if (currentWorkflow && currentWorkflow.name === wf.name) {
        li.classList.add('active');
      }

      li.innerHTML = `
        <div class="node-info" onclick="window.selectWorkflow('${wf.name}')">
          <span class="node-icon">🌿</span>
          <span class="node-name">${window.escapeHtml(wf.name)}</span>
        </div>
      `;
      listEl.appendChild(li);
    });
  }

  window.selectWorkflow = async function (name) {
    try {
      const res = await fetch(`/botson/api/workflows`);
      if (!res.ok) throw new Error('Failed to fetch details');
      const list = await res.json();
      const wf = list.find(w => w.name === name);
      if (!wf) throw new Error('Workflow not found');

      currentWorkflow = wf;
      renderWorkflowSidebar();

      document.getElementById('workflow-welcome').style.display = 'none';
      document.getElementById('workflow-editor').style.display = 'flex';

      document.getElementById('workflowNameInput').value = wf.name;
      document.getElementById('workflowDescInput').value = wf.description || '';

      // Initialize canvas
      const canvas = document.getElementById('workflowCanvas');
      canvas.innerHTML = '';
      selectedNodeId = null;
      window.closeInspector();

      // Render Nodes
      wf.nodes.forEach(n => {
        createNodeDOM(n);
      });

      drawAllEdges();
    } catch (err) {
      window.showToast(`Error: ${err.message}`, 'error');
    }
  };

  window.newWorkflow = function () {
    currentWorkflow = {
      name: `workflow_${Date.now().toString().slice(-4)}`,
      description: 'A multi-agent orchestration pipeline.',
      nodes: [
        { id: 'start', type: 'start', x: 100, y: 150 }
      ],
      edges: []
    };

    document.getElementById('workflow-welcome').style.display = 'none';
    document.getElementById('workflow-editor').style.display = 'flex';

    document.getElementById('workflowNameInput').value = currentWorkflow.name;
    document.getElementById('workflowDescInput').value = currentWorkflow.description;

    const canvas = document.getElementById('workflowCanvas');
    canvas.innerHTML = '';
    selectedNodeId = null;
    window.closeInspector();

    createNodeDOM(currentWorkflow.nodes[0]);
    drawAllEdges();
  };

  window.addNodeToCanvas = function (type) {
    if (!currentWorkflow) return;

    const id = `${type}_${Date.now().toString().slice(-4)}`;
    const newNode = {
      id: id,
      type: type,
      x: 300,
      y: 150,
      agent_name: type === 'agent' ? (availableAgents[0] || 'agent_botson') : '',
      tool_name: type === 'tool' ? (availableTools[0] || 'saveArtifact') : ''
    };

    currentWorkflow.nodes.push(newNode);
    createNodeDOM(newNode);
    selectNode(id);
    drawAllEdges();
  };

  function createNodeDOM(node) {
    const canvas = document.getElementById('workflowCanvas');
    if (!canvas) return;

    const div = document.createElement('div');
    div.className = `wf-node wf-node-${node.type}`;
    div.id = `node-${node.id}`;
    div.style.left = `${node.x}px`;
    div.style.top = `${node.y}px`;

    // Header title
    let headerText = node.id;
    if (node.type === 'start') headerText = '🎬 Start';

    const header = document.createElement('div');
    header.className = 'wf-node-header';
    header.innerHTML = `
      <span>${window.escapeHtml(headerText)}</span>
      ${node.type !== 'start' ? `<button class="btn-delete-node" onclick="window.removeNode('${node.id}', event)">✕</button>` : ''}
    `;
    div.appendChild(header);

    const body = document.createElement('div');
    body.className = 'wf-node-body';

    if (node.type === 'agent') {
      body.innerHTML = `<span style="font-weight: 500;">Agent:</span> <code style="color:var(--accent-light); font-size:10.5px;">${window.escapeHtml(node.agent_name || 'Not Configured')}</code>`;
    } else if (node.type === 'tool') {
      body.innerHTML = `<span style="font-weight: 500;">Tool:</span> <code style="color:#10B981; font-size:10.5px;">${window.escapeHtml(node.tool_name || 'Not Configured')}</code>`;
    } else {
      body.innerHTML = `<span style="font-style: italic; color:var(--text-muted);">Trigger entrypoint</span>`;
    }
    div.appendChild(body);

    // Binds ports (connecting hubs)
    if (node.type !== 'start') {
      const portIn = document.createElement('div');
      portIn.className = 'node-port port-in';
      portIn.dataset.node = node.id;
      portIn.dataset.portType = 'in';
      portIn.onmousedown = (e) => startLinkDrag(e, portIn);
      div.appendChild(portIn);
    }

    const portOut = document.createElement('div');
    portOut.className = 'node-port port-out';
    portOut.dataset.node = node.id;
    portOut.dataset.portType = 'out';
    portOut.onmousedown = (e) => startLinkDrag(e, portOut);
    div.appendChild(portOut);

    // Node selection
    div.onclick = (e) => {
      if (e.target.classList.contains('btn-delete-node')) return;
      selectNode(node.id);
    };

    // Node drag listener
    div.onmousedown = (e) => {
      if (e.target.classList.contains('node-port') || e.target.classList.contains('btn-delete-node')) return;
      dragNode = node;
      const rect = div.getBoundingClientRect();
      dragOffsetX = e.clientX - rect.left;
      dragOffsetY = e.clientY - rect.top;
      e.stopPropagation();
    };

    canvas.appendChild(div);
  }

  function selectNode(id) {
    selectedNodeId = id;
    document.querySelectorAll('.wf-node').forEach(el => {
      el.classList.toggle('selected', el.id === `node-${id}`);
    });
    openInspector(id);
  }

  window.removeNode = function (id, event) {
    if (event) event.stopPropagation();
    if (!currentWorkflow) return;

    currentWorkflow.nodes = currentWorkflow.nodes.filter(n => n.id !== id);
    currentWorkflow.edges = currentWorkflow.edges.filter(e => e.from !== id && e.to !== id);

    const el = document.getElementById(`node-${id}`);
    if (el) el.remove();

    if (selectedNodeId === id) {
      selectedNodeId = null;
      window.closeInspector();
    }

    drawAllEdges();
  };

  // Node Bounding calculations for link curves
  function getPortCoords(nodeId, type) {
    const nodeEl = document.getElementById(`node-${nodeId}`);
    if (!nodeEl) return { x: 0, y: 0 };

    const portEl = nodeEl.querySelector(`.port-${type}`);
    if (!portEl) return { x: 0, y: 0 };

    const canvas = document.getElementById('workflowCanvas');
    const canvasRect = canvas.getBoundingClientRect();
    const portRect = portEl.getBoundingClientRect();

    return {
      x: (portRect.left + portRect.width / 2) - canvasRect.left,
      y: (portRect.top + portRect.height / 2) - canvasRect.top
    };
  }

  function drawAllEdges() {
    const svg = document.getElementById('workflowSVG');
    if (!svg || !currentWorkflow) return;

    svg.innerHTML = '';

    currentWorkflow.edges.forEach((edge, index) => {
      const fromCoords = getPortCoords(edge.from, 'out');
      const toCoords = getPortCoords(edge.to, 'in');

      // Bezier curve control points
      const dx = Math.abs(toCoords.x - fromCoords.x) * 0.5;
      const pathData = `M ${fromCoords.x} ${fromCoords.y} C ${fromCoords.x + dx} ${fromCoords.y}, ${toCoords.x - dx} ${toCoords.y}, ${toCoords.x} ${toCoords.y}`;

      const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      path.setAttribute('d', pathData);
      path.setAttribute('class', 'connector-line');
      
      // Right-click or hover click option to delete link
      path.onclick = (e) => {
        if (confirm('Delete this connection edge?')) {
          currentWorkflow.edges.splice(index, 1);
          drawAllEdges();
        }
      };

      svg.appendChild(path);
    });
  }

  // Link Drawing handlers
  function startLinkDrag(e, portEl) {
    e.stopPropagation();
    e.preventDefault();

    activeLinkPort = {
      node: portEl.dataset.node,
      type: portEl.dataset.portType
    };

    const canvas = document.getElementById('workflowCanvas');
    const svg = document.getElementById('workflowSVG');

    tempLinkLine = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    tempLinkLine.setAttribute('class', 'connector-line');
    tempLinkLine.setAttribute('style', 'stroke: var(--accent); stroke-dasharray: 4;');
    svg.appendChild(tempLinkLine);

    document.addEventListener('mousemove', dragLink);
    document.addEventListener('mouseup', endLinkDrag);
  }

  function dragLink(e) {
    if (!activeLinkPort || !tempLinkLine) return;

    const canvas = document.getElementById('workflowCanvas');
    const canvasRect = canvas.getBoundingClientRect();

    const startCoords = getPortCoords(activeLinkPort.node, activeLinkPort.type);
    const mouseX = e.clientX - canvasRect.left;
    const mouseY = e.clientY - canvasRect.top;

    let pathData = '';
    if (activeLinkPort.type === 'out') {
      const dx = Math.abs(mouseX - startCoords.x) * 0.5;
      pathData = `M ${startCoords.x} ${startCoords.y} C ${startCoords.x + dx} ${startCoords.y}, ${mouseX - dx} ${mouseY}, ${mouseX} ${mouseY}`;
    } else {
      const dx = Math.abs(startCoords.x - mouseX) * 0.5;
      pathData = `M ${mouseX} ${mouseY} C ${mouseX + dx} ${mouseY}, ${startCoords.x - dx} ${startCoords.y}, ${startCoords.x} ${startCoords.y}`;
    }

    tempLinkLine.setAttribute('d', pathData);
  }

  function endLinkDrag(e) {
    document.removeEventListener('mousemove', dragLink);
    document.removeEventListener('mouseup', endLinkDrag);

    if (tempLinkLine) {
      tempLinkLine.remove();
      tempLinkLine = null;
    }

    // Check if mouse is released over a compatible input/output port
    const element = document.elementFromPoint(e.clientX, e.clientY);
    if (element && element.classList.contains('node-port')) {
      const targetNode = element.dataset.node;
      const targetType = element.dataset.portType;

      if (activeLinkPort.node !== targetNode && activeLinkPort.type !== targetType) {
        // Output -> Input link established!
        const fromNode = activeLinkPort.type === 'out' ? activeLinkPort.node : targetNode;
        const toNode = activeLinkPort.type === 'in' ? activeLinkPort.node : targetNode;

        // Prevent duplicates
        const exists = currentWorkflow.edges.some(edge => edge.from === fromNode && edge.to === toNode);
        if (!exists) {
          currentWorkflow.edges.push({
            from: fromNode,
            to: toNode,
            route: 'default'
          });
        }
      }
    }

    activeLinkPort = null;
    drawAllEdges();
  }

  // Global mouse coordinates trackers
  document.addEventListener('mousemove', (e) => {
    if (dragNode) {
      const canvas = document.getElementById('workflowCanvas');
      const canvasRect = canvas.getBoundingClientRect();

      const newX = Math.round(e.clientX - canvasRect.left - dragOffsetX);
      const newY = Math.round(e.clientY - canvasRect.top - dragOffsetY);

      // Snap or boundaries (prevent dragging nodes out of visible zones)
      dragNode.x = Math.max(0, newX);
      dragNode.y = Math.max(0, newY);

      const nodeEl = document.getElementById(`node-${dragNode.id}`);
      if (nodeEl) {
        nodeEl.style.left = `${dragNode.x}px`;
        nodeEl.style.top = `${dragNode.y}px`;
      }

      drawAllEdges();
    }
  });

  document.addEventListener('mouseup', () => {
    dragNode = null;
  });

  // Node configuration drawer
  function openInspector(id) {
    const node = currentWorkflow.nodes.find(n => n.id === id);
    if (!node) return;

    const drawer = document.getElementById('nodeInspector');
    const content = document.getElementById('inspectorContent');
    const title = document.getElementById('inspectorNodeTitle');

    title.textContent = `Node: ${node.id}`;
    content.innerHTML = '';

    if (node.type === 'start') {
      content.innerHTML = `
        <div style="font-size: 13px; color: var(--text-muted); line-height: 1.4;">
          The workflow starts execution here. Messages typed by the user in the Chat Console will target this start trigger.
        </div>
      `;
    } else if (node.type === 'agent') {
      let optionsHTML = '';
      availableAgents.forEach(aName => {
        optionsHTML += `<option value="${aName}" ${node.agent_name === aName ? 'selected' : ''}>${aName}</option>`;
      });

      content.innerHTML = `
        <div class="form-group">
          <label style="font-size:12px; font-weight:600;">Linked Agent</label>
          <select id="inspectAgentSelect" style="width: 100%; padding: 8px; background: var(--bg); border:1px solid var(--border); border-radius:4px; color:var(--text);">
            ${optionsHTML}
          </select>
        </div>
      `;

      document.getElementById('inspectAgentSelect').onchange = (e) => {
        node.agent_name = e.target.value;
        // Refresh label
        const bodyEl = document.querySelector(`#node-${node.id} .wf-node-body`);
        if (bodyEl) {
          bodyEl.innerHTML = `<span style="font-weight: 500;">Agent:</span> <code style="color:var(--accent-light); font-size:10.5px;">${window.escapeHtml(node.agent_name)}</code>`;
        }
      };
    } else if (node.type === 'tool') {
      let optionsHTML = '';
      availableTools.forEach(tName => {
        optionsHTML += `<option value="${tName}" ${node.tool_name === tName ? 'selected' : ''}>${tName}</option>`;
      });

      content.innerHTML = `
        <div class="form-group">
          <label style="font-size:12px; font-weight:600;">Linked Tool</label>
          <select id="inspectToolSelect" style="width: 100%; padding: 8px; background: var(--bg); border:1px solid var(--border); border-radius:4px; color:var(--text);">
            ${optionsHTML}
          </select>
        </div>
      `;

      document.getElementById('inspectToolSelect').onchange = (e) => {
        node.tool_name = e.target.value;
        // Refresh label
        const bodyEl = document.querySelector(`#node-${node.id} .wf-node-body`);
        if (bodyEl) {
          bodyEl.innerHTML = `<span style="font-weight: 500;">Tool:</span> <code style="color:#10B981; font-size:10.5px;">${window.escapeHtml(node.tool_name)}</code>`;
        }
      };
    }

    drawer.style.display = 'flex';
  }

  window.closeInspector = function () {
    const drawer = document.getElementById('nodeInspector');
    if (drawer) drawer.style.display = 'none';
  };

  window.saveWorkflow = async function () {
    if (!currentWorkflow) return;

    // Validate edge/link paths
    const name = document.getElementById('workflowNameInput').value.trim();
    const desc = document.getElementById('workflowDescInput').value.trim();

    if (!name) {
      window.showToast('Workflow Name is required', 'error');
      return;
    }

    currentWorkflow.name = name;
    currentWorkflow.description = desc;

    try {
      const res = await fetch('/botson/api/workflows', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(currentWorkflow)
      });

      if (!res.ok) {
        const errMsg = await res.text();
        throw new Error(errMsg || 'Failed to save workflow config');
      }

      window.showToast('Workflow saved and deployed successfully!', 'success');
      await window.loadWorkflows();
    } catch (err) {
      window.showToast(`Error: ${err.message}`, 'error');
    }
  };

  window.deleteWorkflow = async function () {
    if (!currentWorkflow) return;
    if (!confirm(`Are you sure you want to delete workflow ${currentWorkflow.name}?`)) return;

    try {
      const res = await fetch(`/botson/api/workflows/${currentWorkflow.name}`, {
        method: 'DELETE'
      });

      if (!res.ok) {
        const errMsg = await res.text();
        throw new Error(errMsg || 'Failed to delete workflow');
      }

      window.showToast('Workflow deleted successfully', 'success');
      currentWorkflow = null;
      document.getElementById('workflow-editor').style.display = 'none';
      document.getElementById('workflow-welcome').style.display = 'block';

      await window.loadWorkflows();
    } catch (err) {
      window.showToast(`Error: ${err.message}`, 'error');
    }
  };
})();
