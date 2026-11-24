import React, { useEffect, useState } from "react";
import { getPageScrollTop, scrollToTopSmooth, subscribePageScroll } from "../smoothScroll";

export const ScrollTopButton: React.FC = () => {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    let raf = 0;
    const sync = () => {
      raf = 0;
      const next = getPageScrollTop() > 340;
      setVisible((prev) => (prev === next ? prev : next));
    };

    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(sync);
    };

    sync();
    const unsubScroll = subscribePageScroll(onScroll);
    return () => {
      unsubScroll();
      if (raf) cancelAnimationFrame(raf);
    };
  }, []);

  return (
    <button
      type="button"
      className={`scroll-top-btn${visible ? " is-visible" : ""}`}
      aria-label="Прокрутить наверх"
      onClick={() => scrollToTopSmooth()}
    >
      <svg className="scroll-top-arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <polyline points="18 15 12 9 6 15" />
      </svg>
    </button>
  );
};
