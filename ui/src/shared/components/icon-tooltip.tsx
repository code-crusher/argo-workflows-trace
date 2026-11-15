import * as React from 'react';

import {Tooltip} from './tooltip';

import './icon-tooltip.scss';

type TooltipProps = React.ComponentProps<typeof Tooltip>;

export interface IconTooltipProps extends Omit<TooltipProps, 'children'> {
    // Font Awesome class(es) for the icon, e.g. 'fa fa-question-circle'.
    icon: string;
    // Accessible name for the trigger button. The icon is hidden from assistive technology,
    // so this is the only name the trigger exposes.
    label: string;
    onClick?: React.MouseEventHandler<HTMLButtonElement>;
    className?: string;
    style?: React.CSSProperties;
}

// An icon-only tooltip trigger that is keyboard-focusable and exposes an accessible name.
// Tippy (via the shared Tooltip) shows the tooltip on focus by default, so the wrapper only
// needs to make the trigger a real button.
export function IconTooltip(props: IconTooltipProps) {
    const {icon, label, onClick, className, style, ...tooltipProps} = props;
    const classes = ['icon-tooltip-trigger'];
    if (className) {
        classes.push(className);
    }
    return (
        <Tooltip {...tooltipProps}>
            <button type='button' className={classes.join(' ')} style={style} aria-label={label} onClick={onClick}>
                <i className={icon} aria-hidden='true' />
            </button>
        </Tooltip>
    );
}
