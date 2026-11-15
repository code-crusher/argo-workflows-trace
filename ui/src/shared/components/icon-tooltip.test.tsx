import {fireEvent, render, screen} from '@testing-library/react';
import React from 'react';

import {IconTooltip} from './icon-tooltip';

// The shared Tooltip is exercised in tooltip.test.tsx; here we mock the argo-ui Tooltip
// to keep the focus on the trigger contract: keyboard-focusable, accessible name, icon
// presentation preserved.
const argoTooltipSpy = jest.fn();
jest.mock('argo-ui/src/components/tooltip/tooltip', () => ({
    Tooltip: (props: {content: React.ReactNode; children?: React.ReactNode}) => {
        argoTooltipSpy(props);
        return (
            <div data-testid='argo-tooltip'>
                <div data-testid='tooltip-content'>{props.content}</div>
                {props.children}
            </div>
        );
    }
}));

describe('IconTooltip', () => {
    beforeEach(() => {
        argoTooltipSpy.mockClear();
    });

    it('renders a keyboard-focusable button with an accessible name', () => {
        render(<IconTooltip content='Some description' icon='fa fa-question-circle' label='Describe parameter foo' />);

        const trigger = screen.getByRole('button', {name: 'Describe parameter foo'});
        expect(trigger.tagName).toBe('BUTTON');
        expect(trigger).toHaveAttribute('type', 'button');
    });

    it('hides the icon from assistive technology so the accessible name comes from the label', () => {
        render(<IconTooltip content='Some description' icon='fa fa-question-circle' label='Describe parameter foo' />);

        const icon = screen.getByRole('button', {name: 'Describe parameter foo'}).querySelector('i');
        expect(icon).toHaveAttribute('aria-hidden', 'true');
        expect(icon).toHaveClass('fa', 'fa-question-circle');
    });

    it('passes the tooltip content and extra props through to the shared Tooltip', () => {
        render(<IconTooltip content='hidden secret' icon='fa fa-key' label='Why hidden?' arrow={false} />);

        expect(argoTooltipSpy).toHaveBeenCalledTimes(1);
        const props = argoTooltipSpy.mock.calls[0][0];
        expect(props.content).toBeDefined();
        expect(props.arrow).toBe(false);
    });

    it('invokes onClick when the trigger is activated', () => {
        const onClick = jest.fn();
        render(<IconTooltip content='Some description' icon='fa fa-question-circle' label='Describe parameter foo' onClick={onClick} />);

        fireEvent.click(screen.getByRole('button', {name: 'Describe parameter foo'}));
        expect(onClick).toHaveBeenCalledTimes(1);
    });

    it('can be triggered from the keyboard', () => {
        render(<IconTooltip content='Some description' icon='fa fa-question-circle' label='Describe parameter foo' />);

        const trigger = screen.getByRole('button', {name: 'Describe parameter foo'});
        trigger.focus();
        expect(trigger).toHaveFocus();
    });
});
