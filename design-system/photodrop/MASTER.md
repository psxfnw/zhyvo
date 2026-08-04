# Product UI System — Zhyvo

Назва **Zhyvo** затверджена. Внутрішній шлях каталогу залишається без змін, щоб не створювати технічного churn.

## Direction

Premium consumer mobile utility inspired by the clarity and material depth of modern Apple interfaces. The UI is light, calm and content-first; glass and glow communicate hierarchy, not decoration.

## Tokens

- Canvas: `#F5F5F7`.
- Glass surface: `rgba(255,255,255,.72)`; solid fallback `#FFFFFF`.
- Elevated tint: `#EEF3FF`.
- Text: `#1D1D1F`; secondary: `#636366`.
- Primary: `#0071E3`; gradient end: `#6E5CFF`.
- Border: `rgba(29,29,31,.10)`; glass highlight: `rgba(255,255,255,.78)`.
- Destructive: `#D70015` only for destructive actions.
- Font: `-apple-system`, `BlinkMacSystemFont`, `SF Pro Display`, `SF Pro Text`, `Inter`, sans-serif.

## Material and shape

- Navigation: 70–90% white glass, 18–24 px backdrop blur.
- Cards: radius 24 px, soft blue shadow, one-pixel translucent border.
- Inputs: radius 14 px, solid high-contrast inner surface.
- Primary controls: pill radius 999 px or 16 px for full-width CTAs.
- Sheets/dialogs: 28–32 px radius with stronger blur and shadow.
- Media: 18–22 px radius with clean edge highlight.

## Signature

The selected room lifetime drives a soft blue/violet aura around the creation card and appears as a tactile range slider. In a room, the same aura sits behind the TTL capsule.

## Interaction

- Native-feeling easing: `cubic-bezier(.16,1,.3,1)`.
- Press: scale to `.975` for 80–120 ms without changing layout bounds.
- Hover/focus: subtle highlight and colored shadow, never aggressive movement.
- Touch target: minimum 44×44 px with 8 px separation.
- All async work exposes visible and live status.
- Respect safe areas and `prefers-reduced-motion`.

## Content

- Standard Ukrainian action labels.
- No fake activity, invented users or ornamental technical language.
- Thumbnails and event information remain the visual focus.

## Validation

- Test 375, 768, 1024, 1440 px and phone landscape.
- Verify no horizontal overflow and 44 px touch targets.
- Maintain WCAG AA contrast inside translucent materials.
- Blur must have a readable opaque fallback.
