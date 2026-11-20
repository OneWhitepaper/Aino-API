# OmniFlow homepage local assets

The three original full-colour provider SVG marks come from @lobehub/icons-static-svg 1.104.0 (MIT). Third-party trademarks remain the property of their respective owners. See LICENSE-lobehub.txt.

Schibsted Grotesk and JetBrains Mono were downloaded from the Google Fonts CSS API on 2026-11-20 and losslessly converted from the original TTF files to WOFF2 with FontTools. They use SIL Open Font License 1.1. The original notices accompany both families. Chinese text uses the visitor's system fonts. No remote font or icon request is needed.

This directory contains only assets used by the native homepage. The site logo is supplied by the existing site configuration.

The local effects/fx.js and effects/grain-gradient/effect.js are unmodified runtime assets from the user-invoked design-taste-frontend skill (tasteskill 2.5.1), preserving the approved prototype’s still grain background. Only the homepage mounts this effect; it destroys the instance when leaving the page. Reduced-motion and unsupported-WebGL visits retain a static CSS background.

The local moments/lib.js and moments/word-highlight/moment.js are also unmodified assets from the same invoked skill. The homepage mounts the original word-highlight module on a dedicated text subtree and destroys its scroll/resize observers on unmount. Its presentation rules are scoped under .omniflow-home.
