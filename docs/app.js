// ---- Scroll reveal ----
const revealEls = document.querySelectorAll(".reveal");
if ("IntersectionObserver" in window) {
  const io = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry, i) => {
        if (entry.isIntersecting) {
          // small stagger for grouped elements
          setTimeout(() => entry.target.classList.add("in"), (i % 4) * 70);
          io.unobserve(entry.target);
        }
      });
    },
    { threshold: 0.12 }
  );
  revealEls.forEach((el) => io.observe(el));
} else {
  revealEls.forEach((el) => el.classList.add("in"));
}

// ---- Copy to clipboard ----
function flash(btn) {
  const original = btn.textContent;
  btn.textContent = "Copied!";
  btn.classList.add("copied");
  setTimeout(() => {
    btn.textContent = original;
    btn.classList.remove("copied");
  }, 1600);
}

document.querySelectorAll("[data-copy]").forEach((container) => {
  const btn = container.querySelector(".copy-btn");
  if (!btn) return;
  btn.addEventListener("click", async () => {
    const text = container.getAttribute("data-copy");
    try {
      await navigator.clipboard.writeText(text);
      flash(btn);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); flash(btn); } catch {}
      document.body.removeChild(ta);
    }
  });
});

// ---- Terminal typing animation ----
const term = document.getElementById("term");
if (term) {
  const lines = [
    { t: '<span class="t-accent">❯</span> goloop run .', d: 320 },
    { t: '<span class="t-dim">supervisor</span> <span class="t-white">chatgpt/gpt-4.1</span>   <span class="t-dim">worker</span> <span class="t-white">cursor/composer-2.5</span>', d: 420 },
    { t: '', d: 120 },
    { t: '<span class="t-dim">── iteration 1 · bootstrap ─────────────</span>', d: 280 },
    { t: '<span class="t-accent">▸ delegate</span>  <span class="t-muted">"Scaffold project &amp; CLI entrypoint"</span>', d: 520 },
    { t: '  <span class="t-green">✓</span> worker done <span class="t-dim">(12s)</span>', d: 380 },
    { t: '', d: 100 },
    { t: '<span class="t-dim">── iteration 2 · build ─────────────────</span>', d: 280 },
    { t: '<span class="t-accent">▸ delegate</span>  <span class="t-muted">"Implement add/list/done commands"</span>', d: 540 },
    { t: '  <span class="t-green">✓</span> worker done <span class="t-dim">(28s)</span>', d: 360 },
    { t: '', d: 100 },
    { t: '<span class="t-dim">── iteration 3 · test ──────────────────</span>', d: 280 },
    { t: '<span class="t-accent">▸ evaluate</span>  <span class="t-muted">"Run tests, verify criteria"</span>', d: 520 },
    { t: '  <span class="t-green">✓</span> all tests passing', d: 360 },
    { t: '', d: 120 },
    { t: '<span class="t-accent">✦ complete</span> <span class="t-white">— objective achieved in 3 iterations</span>', d: 600 },
  ];

  let idx = 0;
  function renderUpTo(n, withCursor) {
    let html = "";
    for (let i = 0; i < n; i++) {
      html += lines[i].t + "\n";
    }
    if (withCursor) html += '<span class="cursor-blink"></span>';
    term.innerHTML = html;
    // keep the latest line in view if content ever exceeds the fixed height
    term.scrollTop = term.scrollHeight;
  }

  function step() {
    if (idx >= lines.length) {
      renderUpTo(lines.length, true);
      return;
    }
    idx++;
    renderUpTo(idx, idx < lines.length);
    setTimeout(step, lines[idx - 1].d);
  }

  // Start after the terminal is on screen (or shortly after load).
  const start = () => setTimeout(step, 500);
  if ("IntersectionObserver" in window) {
    const tio = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting) { start(); tio.disconnect(); }
    }, { threshold: 0.3 });
    tio.observe(term);
  } else {
    start();
  }
}

// ---- Footer year (if present) ----
const yr = document.getElementById("year");
if (yr) yr.textContent = new Date().getFullYear();
