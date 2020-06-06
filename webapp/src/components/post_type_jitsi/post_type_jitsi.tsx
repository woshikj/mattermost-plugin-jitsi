import * as React from 'react';

import {Post} from 'mattermost-redux/types/posts';

import Svgs from '../../constants/svgs';

import {makeStyleFromTheme} from 'mattermost-redux/utils/theme_utils';

//import {CopyToClipboard} from 'react-copy-to-clipboard';
import CopyToClipboard = require("react-copy-to-clipboard");

export type Props = {
    post?: Post,
    theme: any,
    creatorName: string,
    useMilitaryTime: boolean,
    meetingEmbedded: boolean,
    actions: {
        enrichMeetingJwt: (jwt: string) => any,
        openJitsiMeeting: (post: Post | null, jwt: string | null) => void
    }
}

type State = {
    meetingJwt?: string,
}

export class PostTypeJitsi extends React.PureComponent<Props, State> {
    constructor(props: Props) {
        super(props);

        this.state = {};
    }

    componentDidMount() {
        const {post} = this.props;
        if (post && post.props.jwt_meeting) {
            /*this.props.actions.enrichMeetingJwt(post.props.meeting_jwt).then((response: any) => {
                if (response.data) {
                    this.setState({meetingJwt: response.data.jwt});
                }
            });*/
        }
    }

    openJitsiMeeting = (e: React.MouseEvent) => {
        if (this.props.meetingEmbedded) {
            e.preventDefault();
            if (this.props.post) {
                this.props.actions.openJitsiMeeting(this.props.post, this.state.meetingJwt || this.props.post.props.meeting_jwt || null);
            }
        }
    }

    renderUntilDate = (post: Post, style: any): React.ReactNode => {
        const props = post.props;

        if (props.jwt_meeting) {
            const date = new Date(props.jwt_meeting_valid_until * 1000);
            var expired = false;
            let dateStr = props.jwt_meeting_valid_until;
            if (!isNaN(date.getTime())) {
                dateStr = date.toString();
                expired = (date.getTime() - Date.now()) < 0;
            }
            return expired  ?  (<div style={style.validUntil}>{' Meeting link valid until: '} <b>{'Expired'}</b></div>)
                            : (<div style={style.validUntil}>{' Meeting link valid until: '} <b>{dateStr}</b></div>);
        }
        return null;
    }

    render() {
        const style = getStyle(this.props.theme);
        const post = this.props.post;
        if (!post) {
            return null;
        }

        const props = post.props;

        let meetingLink = props.meeting_link;
        let personalLink = props.meeting_link;
        /*if (this.state.meetingJwt) {
            meetingLink += '?jwt=' + this.state.meetingJwt;
        } else if (props.jwt_meeting) {
            meetingLink += '?jwt=' + (props.meeting_jwt);
        }*/
        /*let personalLink = props.meeting_raw_link;
        if (personalLink && this.state.meetingJwt) {
            personalLink += '?jwt=' + this.state.meetingJwt;
        } else if (personalLink && props.jwt_meeting && props.meeting_jwt) {
            personalLink += '?jwt=' + props.meeting_jwt;
        }*/
        if(props.meeting_topic)
            meetingLink += `#config.callDisplayName="${props.meeting_topic}"`;

        const preText = `${this.props.creatorName} has started a meeting`;

        let subtitle = 'Meeting Link: ';
        if (props.meeting_personal) {
            subtitle = 'Personal Meeting ID (PMI): ';
        }

        let title = 'Video Conference';
        if (props.meeting_topic) {
            title = props.meeting_topic;
        }

        return (
            <div>
                {preText}
                <div style={style.attachment}>
                    <div style={style.content}>
                        <div style={style.container}>
                            <h1 style={style.title}>
                                {title}
                            </h1>
                            <span>
                                {subtitle}
                                <a
                                    target='_blank'
                                    rel='noopener noreferrer'
                                    onClick={this.openJitsiMeeting}
                                    href={meetingLink}
                                >
                                    {meetingLink || props.meeting_id}
                                </a>
                            </span>
                            <div>
                                <div style={style.body}>
                                    <div>
                                        <a
                                            className='btn btn-lg btn-primary'
                                            style={style.button}
                                            target='_blank'
                                            rel='noopener noreferrer'
                                            onClick={this.openJitsiMeeting}
                                            href={personalLink || meetingLink}
                                        >
                                            <i
                                                style={style.buttonIcon}
                                                dangerouslySetInnerHTML={{__html: Svgs.VIDEO_CAMERA_3}}
                                            />
                                            {'JOIN MEETING'}
                                        </a>
                                        &nbsp;
                                        <CopyToClipboard onCopy={()=>{alert('Meeting Link copied!');}} text={meetingLink}>
                                            <button className='btn btn-lg btn-primary' style={style.button}>
                                                {'COPY LINK'}
                                            </button>
                                        </CopyToClipboard>
                                    </div>
                                    {this.renderUntilDate(post, style)}
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        );
    }
}

const getStyle = makeStyleFromTheme((theme) => {
    return {
        attachment: {
            marginLeft: '-20px',
            position: 'relative'
        },
        content: {
            borderRadius: '4px',
            borderStyle: 'solid',
            borderWidth: '1px',
            borderColor: '#BDBDBF',
            margin: '5px 0 5px 20px',
            padding: '2px 5px'
        },
        container: {
            borderLeftStyle: 'solid',
            borderLeftWidth: '4px',
            padding: '10px',
            borderLeftColor: '#89AECB'
        },
        body: {
            overflowX: 'auto',
            overflowY: 'hidden',
            paddingRight: '5px',
            width: '100%'
        },
        title: {
            fontSize: '16px',
            fontWeight: '600',
            height: '22px',
            lineHeight: '18px',
            margin: '5px 0 1px 0',
            padding: '0'
        },
        button: {
            fontFamily: 'Open Sans',
            fontSize: '12px',
            fontWeight: 'bold',
            letterSpacing: '1px',
            lineHeight: '19px',
            marginTop: '12px',
            borderRadius: '4px',
            color: theme.buttonColor
        },
        buttonIcon: {
            paddingRight: '8px',
            fill: theme.buttonColor
        },
        validUntil: {
            marginTop: '10px'
        }
    };
});
