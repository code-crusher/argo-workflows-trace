import * as React from 'react';
import {useState} from 'react';

import {IconTooltip} from './icon-tooltip';

export function ClipboardText({text}: {text: string}) {
    const [justClicked, setJustClicked] = useState(false);

    if (!text) {
        return <></>;
    }

    return (
        <>
            {text}
            &nbsp; &nbsp;
            <IconTooltip
                content={justClicked ? 'Copied!' : 'Copy to clipboard'}
                icon='fa fa-clipboard'
                label='Copy to clipboard'
                onClick={() => {
                    setJustClicked(true);
                    navigator.clipboard.writeText(text);
                    setTimeout(() => setJustClicked(false), 2000);
                }}
            />
        </>
    );
}
