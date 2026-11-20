import {
    IconAlertTriangle, IconBolt, IconBranch, IconFile, IconFlag, IconGlobe,
    IconHelpCircle, IconKey, IconLayers, IconSearch, IconServer, IconTickCircle
} from '@douyinfe/semi-icons';
import './graph-card.css';

export const CARD_WIDTH = 216;
export const CARD_HEIGHT = 98;

const NODE_ICONS = {
    target: IconGlobe, infra: IconServer, auth: IconKey, finding: IconSearch, chain: IconBranch,
    exploit: IconBolt, poc: IconTickCircle, vulnerability: IconAlertTriangle, missing: IconHelpCircle,
    goal: IconFlag, step: IconBranch, fact: IconFile, receipt: IconTickCircle, observation: IconSearch,
    memory: IconLayers, digest: IconFile, guidance: IconBolt
};

export function GraphNodeIcon({ type }) {
    const Icon = NODE_ICONS[type] || IconFile;
    return <span className="graph-node-icon" aria-hidden="true"><Icon size="small" /></span>;
}

export function GraphNodeCard({ id, title, type, category, color, factKey, boundary, ports = [], selected, card, onSelect }) {
    const { density, box } = card;
    const detail = density === 'detail';
    const textScale = Math.min(Math.max(box.width / CARD_WIDTH, 1), 1.6);
    return (
        <button
            type="button"
            className={`graph-node graph-node--${density}`}
            title={title}
            aria-label={title}
            aria-pressed={selected}
            data-root={type === 'goal' || type === 'target' || undefined}
            data-boundary={boundary || undefined}
            data-wrap={density === 'standard' && box.height >= 50 || undefined}
            style={{ ...box, '--graph-node-color': color, '--graph-node-text-scale': textScale }}
            onClick={() => onSelect(id)}
        >
            <span className="graph-node-content">
                {density !== 'compact' && (
                    <span className="graph-node-kind">
                        <GraphNodeIcon type={type} />
                        <span>{category}</span>
                    </span>
                )}
                <span className="graph-node-title" dir="auto">{title}</span>
                {detail && box.height >= 84 && factKey && title !== factKey && (
                    <span className="graph-node-keyline" dir="auto">{factKey}</span>
                )}
            </span>
            {detail && [...ports].map(side => (
                <span key={side} className={`graph-node-port is-${side}`} aria-hidden="true" />
            ))}
        </button>
    );
}

export function visibleGraphCards(cy, { anchorId, allowCompact = () => true } = {}) {
    const width = cy.width();
    const height = cy.height();
    const cardWidth = CARD_WIDTH * cy.zoom();
    const cardHeight = CARD_HEIGHT * cy.zoom();
    const density = cardHeight >= 68 ? 'detail' : cardHeight >= 36 ? 'standard' : 'compact';
    const cards = [];
    for (const node of cy.nodes(':visible')) {
        const id = node.id();
        if (node.hasClass('dim')) continue;
        if (density === 'compact' && id !== anchorId && (cardWidth < 49 || !allowCompact(node))) continue;
        const { x, y } = node.renderedPosition();
        const left = x - cardWidth / 2;
        const top = y - cardHeight / 2;
        if (left + cardWidth <= 0 || left >= width || top + cardHeight <= 0 || top >= height) continue;
        let box = { left, top, width: cardWidth, height: cardHeight };
        // Keep the assessment goal readable in very small fitted overviews.
        if (id === anchorId && cardWidth < 49) {
            box = { left: Math.max(3, left), top: y - 13, width: Math.min(108, width - Math.max(3, left)), height: 26 };
        }
        cards.push({ id, density, box });
    }
    return cards;
}

export function zoomGraph(cy, factor, renderedPosition = { x: cy.width() / 2, y: cy.height() / 2 }) {
    cy.zoom({ level: cy.zoom() * factor, renderedPosition });
}

export function bindGraphCardWheel(layer, cy) {
    const wheel = event => {
        if (!event.deltaY) return;
        event.preventDefault();
        const { left, top, height } = layer.getBoundingClientRect();
        const units = event.deltaMode === 1 ? 33 : event.deltaMode === 2 ? height : 1;
        zoomGraph(cy, 10 ** (-event.deltaY * units / 1250), { x: event.clientX - left, y: event.clientY - top });
    };
    // Card buttons sit above the renderer. Keep pointer-centered zoom on either surface.
    layer.addEventListener('wheel', wheel, { passive: false });
    return () => layer.removeEventListener('wheel', wheel);
}
