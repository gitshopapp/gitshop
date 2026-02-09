(() => {
  "use strict";

  const toggleEmailFields = (provider) => {
    document.querySelectorAll("[data-email-fields]").forEach((el) => {
      el.classList.add("hidden");
    });
    if (!provider) {
      return;
    }
    const target = document.querySelector(`[data-email-fields="${provider}"]`);
    if (target) {
      target.classList.remove("hidden");
    }
  };

  const bindEmailProvider = () => {
    const providerInput = document.querySelector(
      'input[name="provider"][data-tui-selectbox-hidden-input]'
    );
    if (!providerInput) {
      return;
    }
    const update = () => toggleEmailFields(providerInput.value);
    if (providerInput.dataset.emailBound === "true") {
      update();
      return;
    }
    providerInput.dataset.emailBound = "true";
    providerInput.addEventListener("change", update);
    update();
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bindEmailProvider);
  } else {
    bindEmailProvider();
  }

  document.addEventListener("htmx:afterSwap", bindEmailProvider);
})();
