const triggers = [...document.querySelectorAll('.feature-trigger')];
let active = null;
let openTimer;
let closeTimer;
let suppressFocus = false;
let positionFrame;
const isMouse = event => event.pointerType === 'mouse';
const insideActive = node => active && node instanceof Node && (active.button.contains(node) || active.panel.contains(node));

function cancelTimers() {
  clearTimeout(openTimer);
  clearTimeout(closeTimer);
}

function position() {
  if (!active) return;
  const {button, panel} = active;
  const anchor = button.getBoundingClientRect();
  const viewport = window.visualViewport;
  const left = viewport?.offsetLeft || 0;
  const top = viewport?.offsetTop || 0;
  const width = viewport?.width || innerWidth;
  const height = viewport?.height || innerHeight;
  const margin = width <= 760 ? 12 : 16;
  const available = Math.max(80, height - 2 * margin);
  panel.style.maxWidth = `${width - margin * 2}px`;
  panel.style.maxHeight = `${available}px`;
  const panelWidth = panel.getBoundingClientRect().width;
  const panelLeft = Math.max(left + margin, Math.min(anchor.left + anchor.width / 2 - panelWidth / 2, left + width - panelWidth - margin));
  const sentenceBottom = document.querySelector('.feature-lead').getBoundingClientRect().bottom;
  // Keep the trigger reachable; short landscape screens scroll inside the preview.
  const panelTop = Math.max(top + margin, Math.min(Math.max(anchor.bottom, sentenceBottom) + 10, top + height - Math.min(190, available) - margin));
  panel.style.left = `${Math.round(panelLeft)}px`;
  panel.style.top = `${Math.round(panelTop)}px`;
  panel.style.maxHeight = `${Math.floor(top + height - panelTop - margin)}px`;
}

function close({restoreFocus = false} = {}) {
  cancelTimers();
  if (!active) return;
  const {button, panel} = active;
  active = null;
  button.setAttribute('aria-expanded', 'false');
  panel.hidden = true;
  if (restoreFocus) {
    suppressFocus = true;
    button.focus({preventScroll:true});
    suppressFocus = false;
  }
}

function open(button, mode) {
  cancelTimers();
  if (active?.button === button) {
    if (mode === 'click') active.mode = 'click';
    return;
  }
  close();
  const panel = document.getElementById(button.getAttribute('aria-controls'));
  active = {button, panel, mode};
  for (const image of panel.querySelectorAll('img[data-src]')) {
    image.src = image.dataset.src;
    delete image.dataset.src;
  }
  panel.hidden = false;
  panel.scrollTop = 0;
  button.setAttribute('aria-expanded', 'true');
  position();
}

function scheduleClose() {
  clearTimeout(openTimer);
  if (active?.mode !== 'hover') return;
  closeTimer = setTimeout(() => {
    if (!active || active.button.matches(':hover') || active.panel.matches(':hover') || active.panel.contains(document.activeElement)) return;
    close();
  }, 220);
}

for (const button of triggers) {
  const panel = document.getElementById(button.getAttribute('aria-controls'));
  button.addEventListener('pointerenter', event => {
    if (!isMouse(event)) return;
    cancelTimers();
    openTimer = setTimeout(() => open(button, 'hover'), 140);
  });
  button.addEventListener('pointerleave', scheduleClose);
  button.addEventListener('focus', () => {
    if (!suppressFocus && button.matches(':focus-visible')) open(button, 'focus');
  });
  button.addEventListener('click', () => {
    if (active?.button === button && active.mode === 'click') close();
    else open(button, 'click');
  });
  panel.addEventListener('pointerenter', cancelTimers);
  panel.addEventListener('pointerleave', scheduleClose);
  panel.querySelector('.preview-close').addEventListener('click', () => close({restoreFocus:true}));
  for (const image of panel.querySelectorAll('img')) {
    image.addEventListener('load', () => { if (active?.panel === panel) position(); });
    image.addEventListener('error', () => {
      image.hidden = true;
      if (image.nextElementSibling?.classList.contains('preview-image-error')) return;
      const notice = document.createElement('p');
      notice.className = 'preview-image-error';
      notice.textContent = '图片暂未加载，请刷新页面后再试。';
      image.after(notice);
    });
  }
}

document.addEventListener('pointerdown', event => {
  if (active && !insideActive(event.target)) close();
});
document.addEventListener('focusin', event => {
  if (active && !insideActive(event.target)) close();
});
document.addEventListener('keydown', event => {
  if (!active) return;
  if (event.key === 'Escape') {
    event.preventDefault();
    event.stopImmediatePropagation();
    close({restoreFocus:true});
  } else if (event.key === 'Tab') {
    // Panels follow the full sentence in the DOM; bridge that gap for keyboard users.
    if (event.target === active.button && !event.shiftKey) {
      event.preventDefault();
      active.panel.querySelector('.preview-close').focus();
    } else if (active.panel.contains(event.target)) {
      const controls = [...active.panel.querySelectorAll('button,a[href]')];
      if (event.shiftKey && event.target === controls[0]) {
        event.preventDefault();
        active.button.focus();
      } else if (!event.shiftKey && event.target === controls.at(-1)) {
        event.preventDefault();
        const next = triggers[triggers.indexOf(active.button) + 1] || document.getElementById('install-prompt-text');
        close();
        next.focus();
      }
    }
  }
}, true);

function schedulePosition() {
  cancelAnimationFrame(positionFrame);
  positionFrame = requestAnimationFrame(position);
}
window.addEventListener('resize', schedulePosition);
window.addEventListener('scroll', schedulePosition, {passive:true});
window.visualViewport?.addEventListener('resize', schedulePosition);
window.visualViewport?.addEventListener('scroll', schedulePosition);
