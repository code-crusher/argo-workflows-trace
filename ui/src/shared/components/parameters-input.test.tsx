import {render, screen} from '@testing-library/react';
import React from 'react';

import {ParametersInput} from './parameters-input';

// Keep the Tooltip transparent: render children and surface the content as
// text so tests can assert the accessible trigger wiring without tippy.
jest.mock('argo-ui/src/components/tooltip/tooltip', () => ({
    Tooltip: (props: {content: React.ReactNode; children: React.ReactNode}) => (
        <span data-testid='tooltip'>
            {props.children}
            <span data-testid='tooltip-content'>{props.content}</span>
        </span>
    )
}));

describe('ParametersInput', () => {
    it('renders an accessible description trigger for parameters with a description', () => {
        const parameters = [{name: 'image', description: 'image to run', value: 'nginx'}];
        render(<ParametersInput parameters={parameters} onChange={jest.fn()} />);

        expect(screen.getByRole('button', {name: 'Describe parameter image'})).toBeInTheDocument();
        expect(screen.getByText('image to run')).toBeInTheDocument();
    });

    it('does not render a description trigger for parameters without a description', () => {
        const parameters = [{name: 'image', value: 'nginx'}];
        render(<ParametersInput parameters={parameters} onChange={jest.fn()} />);

        expect(screen.queryByRole('button', {name: /Describe parameter/})).not.toBeInTheDocument();
    });
});
