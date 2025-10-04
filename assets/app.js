let sloubiCount = 0;
let expectedSloubiNumber = parseInt(localStorage.getItem('expected_sloubi_number')) || 1;
let cheatModeEnabled = false;
let deferredPrompt = null;
let nickname = localStorage.getItem('sloubi_nickname') || '';
let ws = null;
let reconnectInterval = null;

const chatMessages = document.getElementById('chatMessages');
const chatInput = document.getElementById('chatInput');
const sendButton = document.getElementById('sendButton');
const sloubiCountElement = document.getElementById('sloubiCount');
const settingsButton = document.getElementById('settingsButton');
const settingsModal = document.getElementById('settingsModal');
const closeSettings = document.getElementById('closeSettings');
const cheatModeToggle = document.getElementById('cheatModeToggle');
const cheatButton = document.getElementById('cheatButton');
const quickSloubi = document.getElementById('quickSloubi');
const pwaInstallSection = document.getElementById('pwaInstallSection');
const installButton = document.getElementById('installButton');
const nicknameInput = document.getElementById('nicknameInput');

// Load nickname from localStorage
nicknameInput.value = nickname;

// Save nickname to localStorage
nicknameInput.addEventListener('input', (e) => {
    nickname = e.target.value.trim();
    localStorage.setItem('sloubi_nickname', nickname);
});

function addMessage(text, isSloubi = false) {
    const messageDiv = document.createElement('div');
    messageDiv.className = isSloubi ? 'message sloubi-message' : 'message';
    
    if (nickname) {
        const nicknameSpan = document.createElement('span');
        nicknameSpan.className = 'message-nickname';
        nicknameSpan.textContent = nickname + ': ';
        messageDiv.appendChild(nicknameSpan);
        
        const textSpan = document.createElement('span');
        textSpan.textContent = text;
        messageDiv.appendChild(textSpan);
    } else {
        messageDiv.textContent = text;
    }
    
    chatMessages.appendChild(messageDiv);
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

function addSystemMessage(text) {
    const messageDiv = document.createElement('div');
    messageDiv.className = 'system-message';
    messageDiv.textContent = text;
    chatMessages.appendChild(messageDiv);
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

function countSloubi(text) {
    // Check if message contains "sloubi" followed by the expected number
    const sloubiPattern = new RegExp(`sloubi\\s*${expectedSloubiNumber}\\b`, 'i');
    
    if (sloubiPattern.test(text)) {
        expectedSloubiNumber++;
        return 1;
    }
    
    return 0;
}

function updateCounter(count) {
    sloubiCount += count;
    sloubiCountElement.textContent = sloubiCount;
    
    // Save expected number to localStorage
    localStorage.setItem('expected_sloubi_number', expectedSloubiNumber);
    
    // Update cheat button text if cheat mode is enabled
    updateCheatButtonText();
    
    // Animation du compteur
    sloubiCountElement.style.transform = 'scale(1.2)';
    setTimeout(() => {
        sloubiCountElement.style.transform = 'scale(1)';
    }, 200);
}

function updateCheatButtonText() {
    if (cheatModeEnabled) {
        quickSloubi.textContent = `⚡ sloubi ${expectedSloubiNumber}`;
    }
}

function sendMessage() {
    const message = chatInput.value.trim();

    if (message === '') return;

    const sloubiInMessage = countSloubi(message);

    // Send to backend via REST
    fetch('/verif.json', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            clientId,
            nickname: nickname || 'Anonyme',
            message: message,
            timestamp: new Date().toISOString()
        })
    }).catch(err => {
        console.log('Backend not available:', err);
    });

    chatInput.value = '';
}

sendButton.addEventListener('click', sendMessage);

chatInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
        sendMessage();
    }
});

// Messages d'ambiance aléatoires
const ambientMessages = [
    "🍺 On entend le bruit des chopes qui s'entrechoquent...",
    "🔥 Le feu crépite dans la cheminée...",
    "🎵 Un ménestrel joue une ballade au loin...",
    "👥 Des voyageurs discutent à voix basse...",
    "🌙 La nuit est tombée sur la Bretagne...",
    "⚔️ Un chevalier entre dans la taverne...",
    "🐀 Un rat traverse furtivement la pièce..."
];

function addAmbientMessage() {
    if (Math.random() > 0.3) {
        const randomMessage = ambientMessages[Math.floor(Math.random() * ambientMessages.length)];
        addSystemMessage(randomMessage);
    }
    
    const nextDelay = 15000 + Math.random() * 25000;
    setTimeout(addAmbientMessage, nextDelay);
}

setTimeout(addAmbientMessage, 10000);

// Focus automatique sur l'input
chatInput.focus();

// Transition de style du compteur
sloubiCountElement.style.transition = 'transform 0.2s ease';

// Settings modal
settingsButton.addEventListener('click', () => {
    settingsModal.classList.add('active');
});

closeSettings.addEventListener('click', () => {
    settingsModal.classList.remove('active');
});

settingsModal.addEventListener('click', (e) => {
    if (e.target === settingsModal) {
        settingsModal.classList.remove('active');
    }
});

// Cheat mode toggle
cheatModeToggle.addEventListener('change', (e) => {
    cheatModeEnabled = e.target.checked;
    cheatButton.style.display = cheatModeEnabled ? 'block' : 'none';
    
    if (cheatModeEnabled) {
        updateCheatButtonText();
        addSystemMessage('🎮 Mode Triche activé ! Utilisez le bouton rapide.');
    }
});

// Quick sloubi button
quickSloubi.addEventListener('click', () => {
    chatInput.value = `sloubi ${expectedSloubiNumber}`;
    sendMessage();
});

// PWA Installation
window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault();
    deferredPrompt = e;
    pwaInstallSection.style.display = 'flex';
});

installButton.addEventListener('click', async () => {
    if (!deferredPrompt) {
        // For iOS Safari
        if (isIOS() && !isInStandaloneMode()) {
            alert('Pour installer cette app sur iOS:\n1. Appuyez sur le bouton Partager\n2. Sélectionnez "Sur l\'écran d\'accueil"');
        }
        return;
    }
    
    deferredPrompt.prompt();
    const { outcome } = await deferredPrompt.userChoice;
    
    if (outcome === 'accepted') {
        addSystemMessage('✅ Application installée avec succès !');
    }
    
    deferredPrompt = null;
    pwaInstallSection.style.display = 'none';
});

function isIOS() {
    return /iPhone|iPad|iPod/.test(navigator.userAgent) && !window.MSStream;
}

function isInStandaloneMode() {
    return window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone;
}

// Check if already installed
if (isInStandaloneMode()) {
    pwaInstallSection.style.display = 'none';
} else if (isIOS()) {
    // Show install section for iOS
    pwaInstallSection.style.display = 'flex';
    installButton.textContent = 'Voir instructions';
}

// WebSocket connection
function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log('WebSocket connected');
        addSystemMessage('🔗 Connecté au serveur');
        if (reconnectInterval) {
            clearInterval(reconnectInterval);
            reconnectInterval = null;
        }
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);

            // Display message from other users
            const isOwnMessage = data.nickname === nickname;

            if (data.sloubiCount === 1) {
                const messageDiv = document.createElement('div');
                messageDiv.className = 'message sloubi-message';

                const nicknameSpan = document.createElement('span');
                nicknameSpan.className = 'message-nickname';
                nicknameSpan.textContent = data.nickname + ': ';
                messageDiv.appendChild(nicknameSpan);

                const textSpan = document.createElement('span');
                textSpan.textContent = data.message;
                messageDiv.appendChild(textSpan);

                chatMessages.appendChild(messageDiv);
                chatMessages.scrollTop = chatMessages.scrollHeight;

                // Update counter and expected number
                sloubiCount++;
                expectedSloubiNumber++;
                sloubiCountElement.textContent = sloubiCount;
                localStorage.setItem('expected_sloubi_number', expectedSloubiNumber);
                updateCheatButtonText();

                // Animation
                sloubiCountElement.style.transform = 'scale(1.2)';
                setTimeout(() => {
                    sloubiCountElement.style.transform = 'scale(1)';
                }, 200);

                setTimeout(() => {
                    addSystemMessage(`✨ Sloubi ${expectedSloubiNumber - 1} validé par ${data.nickname} !`);
                }, 300);
            } else {
                const messageDiv = document.createElement('div');
                messageDiv.className = 'message';

                const nicknameSpan = document.createElement('span');
                nicknameSpan.className = 'message-nickname';
                nicknameSpan.textContent = data.nickname + ': ';
                messageDiv.appendChild(nicknameSpan);

                const textSpan = document.createElement('span');
                textSpan.textContent = data.message;
                messageDiv.appendChild(textSpan);

                chatMessages.appendChild(messageDiv);
                chatMessages.scrollTop = chatMessages.scrollHeight;

                if (data.message.toLowerCase().includes('sloubi')) {
                    setTimeout(() => {
                        addSystemMessage(`❌ ${data.nickname}: Nombre incorrect ! Le prochain sloubi attendu est ${expectedSloubiNumber}.`);
                    }, 300);
                }
            }
              if (typeof data.sloubiNumber === 'number') {
                // update hero banner
                const title = document.getElementById('last-sloubi-title');
                const meta  = document.getElementById('last-sloubi-meta');
                const text  = document.getElementById('last-sloubi-text');
                if (title && meta && text) {
                title.textContent = `Sloubi ${data.sloubiNumber}`;
                meta.textContent  = `${data.nickname} — ${new Date(data.timestamp).toLocaleString()}`;
                text.textContent  = data.message || '';
                }
            }

        } catch (e) {
            console.error('Error parsing WebSocket message:', e);
        }
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };

    ws.onclose = () => {
        console.log('WebSocket disconnected');
        addSystemMessage('⚠️ Déconnecté du serveur. Reconnexion...');

        // Reconnect after 3 seconds
        if (!reconnectInterval) {
            reconnectInterval = setTimeout(() => {
                connectWebSocket();
            }, 3000);
        }
    };
}

// Initialize WebSocket connection
connectWebSocket();

async function showLastSloubi() {
  try {
    const r = await fetch('/last-sloubi.json', { cache: 'no-store' });
    const j = await r.json();
    const title = document.getElementById('last-sloubi-title');
    const meta  = document.getElementById('last-sloubi-meta');
    const text  = document.getElementById('last-sloubi-text');

    if (!j.exists) {
      title.textContent = 'Sloubi 0';
      meta.textContent  = 'Pas encore de sloubi. Fais péter le premier.';
      text.textContent  = '';
      return;
    }
    title.textContent = `Sloubi ${j.number}`;
    meta.textContent  = `${j.nickname} — ${new Date(j.timestamp).toLocaleString()}`;
    text.textContent  = j.message || '';
  } catch {
    const t = document.getElementById('last-sloubi-title');
    if (t) t.textContent = 'Sloubi 0';
  }
}
showLastSloubi();
