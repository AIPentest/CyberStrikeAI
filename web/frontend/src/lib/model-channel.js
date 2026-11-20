export function modelChannelName(id, channel) {
    return channel?.name?.trim() || id;
}

export function modelChannelOptions(channels) {
    const options = channels.map(([value, channel]) => ({ value, name: modelChannelName(value, channel) }));
    const counts = new Map();
    for (const { name } of options) {
        counts.set(name, (counts.get(name) || 0) + 1);
    }
    return options.map(({ value, name }) => ({
        value,
        name,
        label: counts.get(name) > 1 ? `${name} · ID ${value}` : name
    }));
}

export function selectedModelChannelName(option) {
    return option.name || option.label;
}
