import {render, waitFor} from '@testing-library/react';
import {createMemoryHistory} from 'history';
import React from 'react';
import {MemoryRouter} from 'react-router-dom';

import {Context, ContextApis} from '../shared/context';
import {WorkflowTemplate} from '../shared/models';
import {services} from '../shared/services';
import {WorkflowTemplateList} from './workflow-template-list';

jest.mock('../shared/components/suspense-monaco-editor', () => ({
    SuspenseMonacoEditor: () => <textarea data-testid='editor' />
}));
jest.mock('../shared/services/requests');
jest.mock('../shared/services/workflow-template-service', () => ({
    WorkflowTemplateService: {list: jest.fn()}
}));

function template(name: string, namespace: string): WorkflowTemplate {
    return {metadata: {name, namespace, creationTimestamp: '2024-01-01T00:00:00Z'}, spec: {}};
}

describe('WorkflowTemplateList', () => {
    beforeEach(() => {
        jest.clearAllMocks();
        jest.spyOn(services.info, 'collectEvent').mockResolvedValue(undefined as any);
    });

    afterEach(() => jest.restoreAllMocks());

    it('gives same-named templates in different namespaces unique row keys', async () => {
        jest.spyOn(services.workflowTemplate, 'list').mockResolvedValue({
            metadata: {},
            items: [template('build', 'ns-a'), template('build', 'ns-b')]
        } as any);
        const history = createMemoryHistory();
        const context: ContextApis = {navigation: {} as any} as any;
        const consoleError = jest.spyOn(console, 'error').mockImplementation(() => undefined);

        render(
            <Context.Provider value={context}>
                <MemoryRouter>
                    <WorkflowTemplateList match={{params: {namespace: 'ns-a'}} as any} location={{search: ''} as any} history={history} />
                </MemoryRouter>
            </Context.Provider>
        );

        await waitFor(() => expect(document.querySelectorAll('.workflow-templates-list__row-container')).toHaveLength(2));
        expect(consoleError.mock.calls.filter(args => args.some(arg => typeof arg === 'string' && arg.includes('Encountered two children with the same key')))).toEqual([]);
    });
});
