// Applies the saved colour theme before the first paint. A classic script in <head> (not a module,
// not deferred), so a dark page never flashes light. The key and the rules are those of
// src/shared/theme.ts, which takes over once the app runs: keep the two in step.
(function () {
  var choice = 'system';
  try {
    var saved = window.localStorage.getItem('goal-theme');
    if (saved === 'light' || saved === 'dark') choice = saved;
  } catch (e) {
    // Storage refused (blocked cookies, private mode): follow the system.
  }
  var prefersDark = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)').matches : false;
  var theme = choice === 'system' ? (prefersDark ? 'dark' : 'light') : choice;
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
})();
