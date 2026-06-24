(function () {
  document.querySelectorAll('.copy-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.getAttribute('data-copy') || '';
      if (!navigator.clipboard) {
        var ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        try { document.execCommand('copy'); } catch (e) {}
        document.body.removeChild(ta);
      } else {
        navigator.clipboard.writeText(text).catch(function () {});
      }
      var orig = btn.textContent;
      btn.textContent = 'copied';
      btn.classList.add('copied');
      setTimeout(function () {
        btn.textContent = orig;
        btn.classList.remove('copied');
      }, 1200);
    });
  });

  var bars = document.querySelectorAll('.savings-chart .bar');
  if ('IntersectionObserver' in window && bars.length) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) {
          e.target.style.opacity = '1';
          e.target.style.transform = 'scaleY(1)';
          io.unobserve(e.target);
        }
      });
    }, { threshold: 0.2 });
    bars.forEach(function (b) {
      b.style.opacity = '0';
      b.style.transform = 'scaleY(0)';
      b.style.transformOrigin = 'bottom';
      b.style.transition = 'opacity .5s ease, transform .6s cubic-bezier(.2,.8,.2,1)';
      io.observe(b);
    });
  }
})();