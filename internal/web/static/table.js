// ailog /table page — vanilla JS for column visibility + row navigation.
// No framework. Runs once on page load, attaches a few listeners, done.
//
// Persistence: localStorage key "ailog:cols:hidden" → comma-separated
// list of column keys the user has chosen to hide. Read on load,
// written on every chooser toggle.

(function () {
  const STORAGE_KEY = 'ailog:cols:hidden';
  const table = document.getElementById('ailog-table');
  if (!table) return;

  // ---- column visibility ----
  const defaultHidden = () => {
    // Any chooser checkbox that starts UNCHECKED is hidden by default.
    const list = document.querySelectorAll('.col-chooser__list input[type="checkbox"]');
    const out = [];
    list.forEach((cb) => { if (!cb.defaultChecked) out.push(cb.value); });
    return out;
  };

  const readHidden = () => {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return defaultHidden();
    return raw.split(',').filter(Boolean);
  };

  const writeHidden = (keys) => {
    localStorage.setItem(STORAGE_KEY, keys.join(','));
  };

  const applyHidden = (keys) => {
    // Drop every existing hide-col-* class, then reapply.
    [...table.classList].forEach((cls) => {
      if (cls.startsWith('hide-col-')) table.classList.remove(cls);
    });
    keys.forEach((k) => table.classList.add('hide-col-' + k));
    // Sync the checkboxes.
    document.querySelectorAll('.col-chooser__list input[type="checkbox"]').forEach((cb) => {
      cb.checked = !keys.includes(cb.value);
    });
  };

  let hidden = readHidden();
  applyHidden(hidden);

  document.querySelectorAll('.col-chooser__list input[type="checkbox"]').forEach((cb) => {
    cb.addEventListener('change', () => {
      const idx = hidden.indexOf(cb.value);
      if (cb.checked && idx !== -1) hidden.splice(idx, 1);
      if (!cb.checked && idx === -1) hidden.push(cb.value);
      writeHidden(hidden);
      applyHidden(hidden);
    });
  });

  // ---- "Show all / Default / Hide all" bulk actions ----
  document.querySelectorAll('.col-chooser__footer [data-col-action]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const action = btn.dataset.colAction;
      if (action === 'all') hidden = [];
      else if (action === 'none') {
        hidden = [];
        document.querySelectorAll('.col-chooser__list input[type="checkbox"]').forEach((cb) => {
          hidden.push(cb.value);
        });
      } else if (action === 'default') {
        localStorage.removeItem(STORAGE_KEY);
        hidden = defaultHidden();
      }
      writeHidden(hidden);
      applyHidden(hidden);
    });
  });

  // ---- click anywhere on a row → navigate ----
  // We can't wrap <tr> in <a>, so pick up data-href on tr and handle it.
  // Skip when the click originated in an interactive element (input,
  // button, anchor) — those should do their own thing.
  table.addEventListener('click', (e) => {
    const tr = e.target.closest('tr[data-href]');
    if (!tr) return;
    if (e.target.closest('a, button, input, label, select, summary')) return;
    window.location.href = tr.dataset.href;
  });

  // ---- close the chooser when clicking outside ----
  const chooser = document.querySelector('.col-chooser');
  if (chooser) {
    document.addEventListener('click', (e) => {
      if (!chooser.open) return;
      if (chooser.contains(e.target)) return;
      chooser.open = false;
    });
  }

  // ---- keep sort+dir hidden fields in the filter form in sync ----
  // When the user changes a filter, htmx fires the form's hx-get with
  // all form fields. The hidden sort/dir fields mirror the current
  // URL so sort doesn't reset to default on filter change.
  const form = document.getElementById('ailog-filter-form');
  if (form) {
    const params = new URLSearchParams(window.location.search);
    const sortInput = form.querySelector('input[name="sort"]');
    const dirInput = form.querySelector('input[name="dir"]');
    if (sortInput) sortInput.value = params.get('sort') || '';
    if (dirInput) dirInput.value = params.get('dir') || '';
  }
})();
